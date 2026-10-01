import assert from 'node:assert/strict'
import { writeFile, unlink } from 'node:fs/promises'
import { fileURLToPath } from 'node:url'
import test, { after } from 'node:test'
import { build } from 'esbuild'
import { JSDOM } from 'jsdom'

const dom = new JSDOM('<!doctype html><html><body></body></html>', { url: 'http://localhost:8080/' })
globalThis.window = dom.window
globalThis.document = dom.window.document
globalThis.HTMLElement = dom.window.HTMLElement
globalThis.Event = dom.window.Event
globalThis.MouseEvent = dom.window.MouseEvent
globalThis.IS_REACT_ACT_ENVIRONMENT = true

// The app keeps its access token in localStorage, so each case controls that
// storage directly rather than relying on a cookie.
const TOKEN_KEY = 'campusclaw.access_token'
const storage = new Map()
globalThis.localStorage = {
  getItem: (key) => (storage.has(key) ? storage.get(key) : null),
  setItem: (key, value) => { storage.set(key, String(value)) },
  removeItem: (key) => { storage.delete(key) },
  clear: () => { storage.clear() },
}

const React = await import('react')
const { createRoot } = await import('react-dom/client')
const { act } = React
const entry = fileURLToPath(new URL('../src/App.tsx', import.meta.url))
const bundleURL = new URL('./.app-test-bundle.mjs', import.meta.url)
const bundlePath = fileURLToPath(bundleURL)
const compiled = await build({
  entryPoints: [entry],
  bundle: true,
  platform: 'node',
  format: 'esm',
  packages: 'external',
  jsx: 'automatic',
  write: false,
  outfile: bundlePath,
})
await writeFile(bundlePath, compiled.outputFiles[0].text)
after(async () => { await unlink(bundlePath).catch(() => {}) })

const teacher = { id: '1', username: 'teacher_a', role: 'teacher', class_id: '1', class_name: 'Class A' }
const student = { ...teacher, id: '2', username: 'student_a1', role: 'student' }
const json = (body, status = 200) => new Response(JSON.stringify(body), {
  status, headers: { 'Content-Type': 'application/json' },
})
const error = (status, code) => json({ error: { code, message: code } }, status)

async function mount(t) {
  const oldFetch = globalThis.fetch
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  const { default: App } = await import(bundleURL.href + '?case=' + Math.random())
  await act(async () => { root.render(React.createElement(App)) })
  t.after(async () => {
    await act(async () => { root.unmount() })
    container.remove()
    globalThis.fetch = oldFetch
  })
  return container
}

async function until(check) {
  for (let i = 0; i < 30; i++) {
    if (check()) return
    await act(async () => { await new Promise((resolve) => setTimeout(resolve, 10)) })
  }
  assert.fail('page did not reach the expected state')
}

test('a stored token restores the server identity for both roles', async (t) => {
  for (const [name, user, upload] of [
    ['teacher', teacher, true],
    ['student', student, false],
  ]) {
    await t.test(name, async (sub) => {
      storage.clear()
      storage.set(TOKEN_KEY, name + '-stored')
      const calls = []
      globalThis.fetch = async (path, init) => {
        calls.push([path, init.headers?.get('Authorization') ?? null])
        if (path === '/api/me') return json(user)
        if (path === '/api/materials') return json({ materials: [] })
        throw Error('unexpected request ' + path)
      }
      const container = await mount(sub)
      await until(() => container.querySelector('header.bar') !== null)
      assert.match(container.querySelector('header.bar').textContent, new RegExp(user.username))
      assert.equal(container.querySelector('form.upload') !== null, upload)
      assert.deepEqual(calls, [
        ['/api/me', 'Bearer ' + name + '-stored'],
        ['/api/materials', 'Bearer ' + name + '-stored'],
      ])
    })
  }
})

test('startup without a stored token shows login and calls nothing', async (t) => {
  storage.clear()
  let calls = 0
  globalThis.fetch = async () => { calls++; return error(401, 'unauthorized') }
  const container = await mount(t)
  await until(() => container.querySelector('form.login') !== null)
  assert.equal(container.querySelector('header.bar'), null)
  assert.equal(calls, 0)
})

test('a rejected token shows login; a transport failure shows a retry control', async (t) => {
  await t.test('rejected token', async (sub) => {
    storage.clear()
    storage.set(TOKEN_KEY, 'expired')
    globalThis.fetch = async () => error(401, 'unauthorized')
    const container = await mount(sub)
    await until(() => container.querySelector('form.login') !== null)
    assert.equal(container.querySelector('header.bar'), null)
    assert.equal(storage.has(TOKEN_KEY), false)
  })

  await t.test('retry after transport failure', async (sub) => {
    storage.clear()
    storage.set(TOKEN_KEY, 'still-good')
    let failing = true
    globalThis.fetch = async (path) => {
      if (path === '/api/me') {
        if (failing) throw Error('offline')
        return json(teacher)
      }
      if (path === '/api/materials') return json({ materials: [] })
      throw Error('unexpected request ' + path)
    }
    const container = await mount(sub)
    await until(() => container.querySelector('main.app button') !== null)
    assert.equal(container.querySelector('form.login'), null)
    // A transport failure must not discard a token that is still valid.
    assert.equal(storage.get(TOKEN_KEY), 'still-good')
    failing = false
    await act(async () => { container.querySelector('main.app button').click() })
    await until(() => container.querySelector('header.bar') !== null)
  })
})

test('login rate limit shows a wait message and keeps the login form', async (t) => {
  storage.clear()
  globalThis.fetch = async (path) => {
    if (path === '/api/login') return error(429, 'rate_limited')
    throw Error('unexpected request ' + path)
  }
  const container = await mount(t)
  await until(() => container.querySelector('form.login') !== null)
  for (const [selector, value] of [['#username', 'teacher_a'], ['#password', 'wrong']]) {
    const input = container.querySelector(selector)
    await act(async () => {
      Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value').set.call(input, value)
      input.dispatchEvent(new window.Event('input', { bubbles: true }))
    })
  }
  await act(async () => {
    container.querySelector('form.login').dispatchEvent(new window.Event('submit', { bubbles: true, cancelable: true }))
  })
  await until(() => container.querySelector('form.login .error') !== null)
  assert.match(container.querySelector('form.login .error').textContent, /稍后|重试/)
  assert.equal(container.querySelector('header.bar'), null)
  assert.equal(storage.has(TOKEN_KEY), false)
})

/** setInput writes a value the way React's onChange expects to see it. */
async function setInput(element, value) {
  const prototype = element.tagName === 'SELECT'
    ? window.HTMLSelectElement.prototype
    : window.HTMLInputElement.prototype
  await act(async () => {
    Object.getOwnPropertyDescriptor(prototype, 'value').set.call(element, value)
    element.dispatchEvent(new window.Event(element.tagName === 'SELECT' ? 'change' : 'input', { bubbles: true }))
  })
}

async function submitForm(element) {
  await act(async () => {
    element.dispatchEvent(new window.Event('submit', { bubbles: true, cancelable: true }))
  })
}

/** signIn mounts the app already signed in as the given account. */
async function signIn(t, user, handle) {
  storage.clear()
  storage.set(TOKEN_KEY, 'session-token')
  globalThis.fetch = async (path, init) => {
    if (path === '/api/me') return json(user)
    // Only the list read is stubbed here; an upload to the same path belongs to
    // the case's own handler.
    if (path === '/api/materials' && (init?.method ?? 'GET') === 'GET') {
      return json({ materials: [] })
    }
    return handle(path, init)
  }
  const container = await mount(t)
  await until(() => container.querySelector('header.bar') !== null)
  return container
}

const hit = (overrides = {}) => ({
  material_id: '4',
  title: '向量讲义',
  chunk_id: '91',
  chunk_index: 1,
  start_offset: 800,
  end_offset: 1580,
  offset_basis: 'extracted',
  excerpt: '本课讲解向量检索的基本原理。',
  source: '/api/materials/4',
  keyword_score: 1.2,
  keyword_rank: 1,
  vector_score: 0.987654321,
  vector_rank: 1,
  rrf_score: 0.032786,
  ...overrides,
})

test('a search shows traceable hits and opens the cited material', async (t) => {
  const searched = []
  const container = await signIn(t, student, async (path, init) => {
    if (path === '/api/search') {
      searched.push(JSON.parse(init.body))
      return json({ query_vector_generated: true, hits: [hit()] })
    }
    if (path === '/api/materials/4') {
      return json({ material: { id: '4', title: '向量讲义' }, content: '完整正文' })
    }
    throw Error('unexpected request ' + path)
  })

  await setInput(container.querySelector('#search-mode'), 'vector')
  await setInput(container.querySelector('#search-query'), '向量检索')
  await submitForm(container.querySelector('form.search'))
  await until(() => container.querySelector('.hit') !== null)

  assert.deepEqual(searched, [{ query: '向量检索', mode: 'vector' }])

  const shown = container.querySelector('.hit').textContent
  assert.match(shown, /向量讲义/)
  assert.match(shown, /切片 2/)
  assert.match(shown, /800–1580/)
  assert.match(shown, /本课讲解向量检索的基本原理/)

  // Only ranks and the fused score are rendered: the vector component itself
  // never reaches the page.
  assert.doesNotMatch(container.textContent, /0\.987654321/)

  await act(async () => { container.querySelector('.hit-actions button').click() })
  await until(() => container.querySelector('.detail-card') !== null)
  assert.match(container.querySelector('.detail-card').textContent, /完整正文/)
})

test('an empty result is a notice, and an outage is a retryable error', async (t) => {
  let mode = 'empty'
  const container = await signIn(t, student, async (path) => {
    if (path !== '/api/search') throw Error('unexpected request ' + path)
    if (mode === 'empty') {
      return json({ query_vector_generated: true, hits: [], message: '资料中未找到相关内容' })
    }
    return error(503, 'service_unavailable')
  })

  await setInput(container.querySelector('#search-query'), '今天天气如何')
  await submitForm(container.querySelector('form.search'))
  await until(() => container.querySelector('.notice[role="status"]') !== null)
  assert.match(container.querySelector('.notice').textContent, /资料中未找到相关内容/)
  assert.equal(container.querySelector('.retrieval-card .error'), null)

  mode = 'outage'
  await submitForm(container.querySelector('form.search'))
  await until(() => container.querySelector('.retrieval-card .error') !== null)
  assert.match(container.querySelector('.retrieval-card .error').textContent, /稍后重试/)
})

test('an answer links its markers to the citation list in the same order', async (t) => {
  const container = await signIn(t, student, async (path) => {
    if (path !== '/api/ask') throw Error('unexpected request ' + path)
    return json({
      answer: '第一点见 [1]，第二点见 [2]，另外 [7] 没有出处。',
      citations: [
        hit({ chunk_id: '1', title: '第一章', chunk_index: 0 }),
        hit({ chunk_id: '2', title: '第二章', chunk_index: 1 }),
      ],
    })
  })

  await setInput(container.querySelector('#ask-question'), '向量检索讲了什么')
  await submitForm(container.querySelector('form.ask'))
  await until(() => container.querySelector('.answer-block') !== null)

  const markers = [...container.querySelectorAll('.citation-link')].map((node) => node.textContent)
  assert.deepEqual(markers, ['[1]', '[2]'])

  const citations = container.querySelectorAll('.citation')
  assert.equal(citations.length, 2)
  assert.match(citations[0].textContent, /\[1\] 第一章/)
  assert.match(citations[1].textContent, /\[2\] 第二章/)

  // Selecting a marker points at that entry and no other.
  await act(async () => { container.querySelectorAll('.citation-link')[1].click() })
  assert.equal(container.querySelectorAll('.citation-active').length, 1)
  assert.match(container.querySelector('.citation-active').textContent, /\[2\] 第二章/)

  // The unverifiable marker stays plain text rather than becoming a control.
  assert.equal(container.querySelectorAll('.citation-link').length, 2)
})

test('only a teacher is offered the index controls', async (t) => {
  const studentView = await signIn(t, student, async (path) => {
    if (path === '/api/materials') return json({ materials: [] })
    throw Error('unexpected request ' + path)
  })
  assert.equal(studentView.querySelector('.index-panel'), null)

  storage.clear()
  storage.set(TOKEN_KEY, 'session-token')
  let indexReads = 0
  globalThis.fetch = async (path) => {
    if (path === '/api/me') return json(teacher)
    if (path === '/api/materials') {
      return json({ materials: [{ id: '4', title: '讲义', original_filename: 'a.md', content_type: 'text/markdown', created_at: '2026-09-30T00:00:00Z' }] })
    }
    if (path === '/api/materials/4/index') {
      indexReads++
      return json({ index: { status: 'ready', generation: 2, strategy: 'hierarchy', chunk_max_chars: 800, chunk_overlap_percent: 10, chunk_separator: 'newline', remove_urls_emails: false, fold_whitespace: false, failure_code: '' } })
    }
    throw Error('unexpected request ' + path)
  }

  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  const { default: App } = await import(bundleURL.href + '?case=' + Math.random())
  await act(async () => { root.render(React.createElement(App)) })
  t.after(async () => {
    await act(async () => { root.unmount() })
    container.remove()
  })

  await until(() => container.querySelector('.index-panel') !== null)

  // Opening the panel is what reads the status: listing material must not fan
  // out into a request per row.
  assert.equal(indexReads, 0)
  await act(async () => { container.querySelector('.index-panel button').click() })
  await until(() => container.querySelector('.index-status') !== null)

  assert.equal(indexReads, 1)
  assert.match(container.querySelector('.index-status').textContent, /已就绪/)
  assert.match(container.querySelector('.index-status').textContent, /第 2 代/)
})

test('an upload carries only the split fields the chosen strategy uses', async (t) => {
  let upload = null
  const container = await signIn(t, teacher, async (path, init) => {
    if (path === '/api/materials' && init?.method === 'POST') {
      upload = init.body
      return json({ material: { id: '9', title: '讲义' } }, 201)
    }
    throw Error('unexpected request ' + path)
  })

  await setInput(container.querySelector('#title'), '讲义')
  await act(async () => {
    const input = container.querySelector('#file')
    const file = new window.File(['# 标题'], 'notes.md', { type: 'text/markdown' })
    // jsdom's files setter insists on a FileList; the component only ever reads
    // files[0], so a plain array is defined directly on the element.
    Object.defineProperty(input, 'files', { value: [file], configurable: true })
    input.dispatchEvent(new window.Event('change', { bubbles: true }))
  })

  // The default strategy sends no length, overlap or separator at all.
  await submitForm(container.querySelector('form.upload'))
  await until(() => upload !== null)
  assert.equal(upload.get('chunk_strategy'), 'auto')
  assert.equal(upload.get('chunk_max_chars'), null)
  assert.equal(upload.get('chunk_separator'), null)
})

test('failed logout leaves the materials page and the stored token usable', async (t) => {
  storage.clear()
  storage.set(TOKEN_KEY, 'still-live')
  const calls = []
  globalThis.fetch = async (path, init) => {
    calls.push([path, init.headers?.get('Authorization') ?? null])
    if (path === '/api/me') return json(teacher)
    if (path === '/api/materials') return json({ materials: [] })
    if (path === '/api/logout') return error(500, 'internal_error')
    throw Error('unexpected request ' + path)
  }
  const container = await mount(t)
  await until(() => container.querySelector('header.bar') !== null)
  await act(async () => { container.querySelector('header.bar button').click() })
  await until(() => container.querySelector('main.materials-page .error') !== null)
  assert.equal(container.querySelector('form.login'), null)
  assert.ok(container.querySelector('header.bar'))
  assert.deepEqual(calls.find(([path]) => path === '/api/logout'), ['/api/logout', 'Bearer still-live'])
  assert.equal(storage.get(TOKEN_KEY), 'still-live')
})
