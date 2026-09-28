// A very small Chrome DevTools Protocol client.
//
// The browser suite drives a real Chromium through this instead of pulling in
// a test framework, so the checks run with no npm dependencies at all: only
// Node's built-in fetch, WebSocket and child_process are used.
//
// It covers exactly what the acceptance checks need: navigate, read text,
// click, type, attach a file, wait for a condition, and observe requests.

import { spawn } from 'node:child_process'
import { mkdtemp, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { setTimeout as delay } from 'node:timers/promises'
import { existsSync } from 'node:fs'

/** defaultExecutablePath finds the Chromium that Playwright's installer left behind. */
export function defaultExecutablePath() {
  const candidates = [
    process.env.CHROME_PATH,
    join(
      process.env.LOCALAPPDATA ?? '',
      'ms-playwright',
      'chromium-1217',
      'chrome-win64',
      'chrome.exe',
    ),
  ].filter(Boolean)

  for (const candidate of candidates) {
    if (existsSync(candidate)) {
      return candidate
    }
  }

  throw new Error(
    'no Chromium found; set CHROME_PATH to a Chrome/Chromium executable',
  )
}

class Connection {
  constructor(url) {
    this.socket = new WebSocket(url)
    this.nextId = 1
    this.pending = new Map()
    this.handlers = new Set()

    this.ready = new Promise((resolve, reject) => {
      this.socket.addEventListener('open', () => resolve())
      this.socket.addEventListener('error', (event) => reject(new Error(`websocket error: ${event.message ?? 'unknown'}`)))
    })

    this.socket.addEventListener('message', (event) => {
      const message = JSON.parse(event.data)

      if (message.id !== undefined) {
        const pending = this.pending.get(message.id)
        if (pending === undefined) {
          return
        }
        this.pending.delete(message.id)
        clearTimeout(pending.timer)

        if (message.error) {
          pending.reject(new Error(`${pending.method}: ${message.error.message}`))
        } else {
          pending.resolve(message.result ?? {})
        }
        return
      }

      for (const handler of this.handlers) {
        handler(message)
      }
    })

    // If the browser goes away, every outstanding call would otherwise hang
    // forever with no reply. Fail them instead, so a crash reads as an error
    // rather than as a stalled run.
    this.socket.addEventListener('close', () => {
      for (const pending of this.pending.values()) {
        clearTimeout(pending.timer)
        pending.reject(new Error(`${pending.method}: the browser connection closed`))
      }
      this.pending.clear()
    })
  }

  onEvent(handler) {
    this.handlers.add(handler)
    return () => this.handlers.delete(handler)
  }

  async send(method, params = {}, sessionId, { timeoutMs = 30_000 } = {}) {
    await this.ready

    const id = this.nextId++
    const payload = { id, method, params }
    if (sessionId !== undefined) {
      payload.sessionId = sessionId
    }

    const promise = new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(id)
        reject(new Error(`${method}: no reply within ${timeoutMs}ms`))
      }, timeoutMs)

      this.pending.set(id, { resolve, reject, method, timer })
    })

    this.socket.send(JSON.stringify(payload))
    return promise
  }

  close() {
    this.socket.close()
  }
}

export class Browser {
  static async launch({ executablePath = defaultExecutablePath(), port = 9333 } = {}) {
    const profile = await mkdtemp(join(tmpdir(), 'campusclaw-chrome-'))

    const child = spawn(
      executablePath,
      [
        `--remote-debugging-port=${port}`,
        `--user-data-dir=${profile}`,
        '--headless=new',
        '--disable-gpu',
        '--no-first-run',
        '--no-default-browser-check',
        '--disable-extensions',
        'about:blank',
      ],
      { stdio: 'ignore' },
    )

    let webSocketUrl = ''

    for (let attempt = 0; attempt < 150; attempt++) {
      if (child.exitCode !== null) {
        throw new Error(`chromium exited early with code ${child.exitCode}`)
      }

      try {
        const response = await fetch(`http://127.0.0.1:${port}/json/version`)
        if (response.ok) {
          webSocketUrl = (await response.json()).webSocketDebuggerUrl
          break
        }
      } catch {
        // Not listening yet.
      }

      await delay(200)
    }

    if (webSocketUrl === '') {
      child.kill()
      throw new Error('chromium never exposed a debugging endpoint')
    }

    const browser = new Browser(child, profile, new Connection(webSocketUrl))
    await browser.connection.send('Target.setDiscoverTargets', { discover: true })

    return browser
  }

  constructor(child, profile, connection) {
    this.child = child
    this.profile = profile
    this.connection = connection
  }

  /** newPage opens an isolated page in its own browser context. */
  async newPage() {
    const { browserContextId } = await this.connection.send('Target.createBrowserContext')
    const { targetId } = await this.connection.send('Target.createTarget', {
      url: 'about:blank',
      browserContextId,
    })
    const { sessionId } = await this.connection.send('Target.attachToTarget', {
      targetId,
      flatten: true,
    })

    const page = new Page(this.connection, sessionId, browserContextId, targetId)

    await page.send('Page.enable')
    await page.send('Runtime.enable')
    await page.send('Network.enable')
    await page.send('DOM.enable')

    return page
  }

  async close() {
    try {
      this.connection.close()
    } catch {
      // Already gone.
    }

    this.child.kill()

    await delay(200)
    await rm(this.profile, { recursive: true, force: true }).catch(() => {})
  }
}

export class Page {
  constructor(connection, sessionId, browserContextId, targetId) {
    this.connection = connection
    this.sessionId = sessionId
    this.browserContextId = browserContextId
    this.targetId = targetId
    this.requests = []
    this.pageErrors = []

    connection.onEvent((message) => {
      if (message.sessionId !== this.sessionId) {
        return
      }

      if (message.method === 'Network.requestWillBeSent') {
        this.requests.push(message.params.request.url)
      }

      if (message.method === 'Runtime.exceptionThrown') {
        this.pageErrors.push(
          message.params.exceptionDetails?.exception?.description ??
            message.params.exceptionDetails?.text ??
            'unknown error',
        )
      }
    })
  }

  send(method, params = {}) {
    return this.connection.send(method, params, this.sessionId)
  }

  /** evaluate runs an expression in the page and returns its value. */
  async evaluate(expression) {
    const result = await this.send('Runtime.evaluate', {
      expression,
      returnByValue: true,
      awaitPromise: true,
    })

    if (result.exceptionDetails) {
      throw new Error(
        `evaluate failed: ${result.exceptionDetails.exception?.description ?? result.exceptionDetails.text}`,
      )
    }

    return result.result?.value
  }

  async goto(url, { timeoutMs = 30_000 } = {}) {
    const loaded = new Promise((resolve) => {
      const stop = this.connection.onEvent((message) => {
        if (message.sessionId === this.sessionId && message.method === 'Page.loadEventFired') {
          stop()
          resolve()
        }
      })
    })

    await this.send('Page.navigate', { url })
    await Promise.race([loaded, delay(timeoutMs)])
  }

  async reload() {
    await this.send('Page.reload', {})
    await delay(500)
    await this.waitForCondition('document.readyState === "complete"')
  }

  async waitForCondition(expression, { timeoutMs = 20_000, label = expression } = {}) {
    const deadline = Date.now() + timeoutMs

    while (Date.now() < deadline) {
      try {
        if (await this.evaluate(expression)) {
          return
        }
      } catch {
        // The page may be mid-navigation.
      }
      await delay(100)
    }

    throw new Error(`timed out waiting for: ${label}`)
  }

  waitForSelector(selector, options) {
    return this.waitForCondition(
      `document.querySelector(${JSON.stringify(selector)}) !== null`,
      { ...options, label: `selector ${selector}` },
    )
  }

  count(selector) {
    return this.evaluate(`document.querySelectorAll(${JSON.stringify(selector)}).length`)
  }

  textContent(selector) {
    return this.evaluate(
      `(document.querySelector(${JSON.stringify(selector)})?.textContent ?? '')`,
    )
  }

  attribute(selector, name) {
    return this.evaluate(
      `(document.querySelector(${JSON.stringify(selector)})?.getAttribute(${JSON.stringify(name)}) ?? null)`,
    )
  }

  /** click dispatches a real click event, which React handles. */
  async click(selector) {
    const clicked = await this.evaluate(`
      (() => {
        const element = document.querySelector(${JSON.stringify(selector)})
        if (element === null) return false
        element.click()
        return true
      })()
    `)

    if (!clicked) {
      throw new Error(`click: selector ${selector} not found`)
    }
  }

  /**
   * typeInto sets an input's value the way a user would, going through the
   * native setter so React's change tracking notices the update.
   */
  async typeInto(selector, value) {
    const typed = await this.evaluate(`
      (() => {
        const element = document.querySelector(${JSON.stringify(selector)})
        if (element === null) return false

        const descriptor = Object.getOwnPropertyDescriptor(
          window.HTMLInputElement.prototype,
          'value',
        )
        descriptor.set.call(element, ${JSON.stringify(value)})
        element.dispatchEvent(new Event('input', { bubbles: true }))
        return true
      })()
    `)

    if (!typed) {
      throw new Error(`typeInto: selector ${selector} not found`)
    }
  }

  /** attachFile points a file input at a real path on disk. */
  async attachFile(selector, filePath) {
    const { root } = await this.send('DOM.getDocument', { depth: -1 })
    const { nodeId } = await this.send('DOM.querySelector', {
      nodeId: root.nodeId,
      selector,
    })

    if (!nodeId) {
      throw new Error(`attachFile: selector ${selector} not found`)
    }

    await this.send('DOM.setFileInputFiles', { files: [filePath], nodeId })
    await this.evaluate(`
      document.querySelector(${JSON.stringify(selector)})
        .dispatchEvent(new Event('change', { bubbles: true }))
    `)
  }
}
