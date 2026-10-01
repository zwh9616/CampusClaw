// Browser end-to-end checks for WE01-WE06 and AC23/AC32.
//
// The scenarios share a single browser page and separate themselves by clearing
// the stored token and reloading, rather than opening a fresh browser context
// each time. That keeps the run stable and mirrors what a user actually does:
// log out, log in as somebody else, reload.
//
// Every URL the browser requests is recorded, so the run doubles as the AC23
// evidence that nothing reaches the API or database containers directly.
//
//   node check.mjs
//
// Configuration:
//   BROWSER_BASE_URL=http://localhost:8080
//   TEACHER_A_PASSWORD / STUDENT_A1_PASSWORD / TEACHER_B_PASSWORD / STUDENT_B1_PASSWORD
//   CHROME_PATH (optional; defaults to the Chromium Playwright installed)

import { mkdtemp, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

import { Browser } from './cdp.mjs'

const BASE = (process.env.BROWSER_BASE_URL ?? 'http://localhost:8080').replace(/\/$/, '')
const BASE_ORIGIN = new URL(BASE).origin

const PASSWORDS = {
  teacher_a: process.env.TEACHER_A_PASSWORD ?? '',
  student_a1: process.env.STUDENT_A1_PASSWORD ?? '',
  teacher_b: process.env.TEACHER_B_PASSWORD ?? '',
  student_b1: process.env.STUDENT_B1_PASSWORD ?? '',
}

for (const [username, password] of Object.entries(PASSWORDS)) {
  if (password === '') {
    console.error(`missing password for ${username}`)
    process.exit(2)
  }
}

const results = []
let current = null

function begin(id, name) {
  current = { id, name, failed: [] }
  results.push(current)
  console.error(`-- ${id}`)
}

function check(condition, message) {
  if (!condition) {
    current.failed.push(message)
  }
}

function report() {
  let failed = 0

  console.log('\nbrowser acceptance results')
  console.log('-'.repeat(72))

  for (const result of results) {
    const status = result.failed.length === 0 ? 'PASS' : 'FAIL'
    if (result.failed.length > 0) {
      failed++
    }

    console.log(`${result.id} ${status.padEnd(5)} ${result.name}`)
    for (const note of result.failed) {
      console.log(`         - ${note}`)
    }
  }

  console.log('-'.repeat(72))
  console.log(`${results.length} checks, ${failed} failed`)

  return failed
}

const timestamp = () => Date.now().toString(36)

// ------------------------------------------------------------- helpers ------

let nextLoginAt = 0

async function paceLogin() {
  const wait = nextLoginAt - Date.now()
  if (wait > 0) {
    await new Promise((resolve) => setTimeout(resolve, wait))
  }
  nextLoginAt = Date.now() + 7_000
}

/**
 * signOut drops the stored token and any leftover cookie.
 *
 * The very first call runs while the page is still on about:blank, where
 * localStorage is unreachable and there is nothing to clear — hence the
 * deliberate swallow. The caller's navigation produces the anonymous view.
 */
async function signOut(page) {
  await page.evaluate('localStorage.clear()').catch(() => {})
  await page.send('Network.clearBrowserCookies')
}

async function signIn(page, username) {
  await signOut(page)
  await page.goto(BASE)
  await page.waitForSelector('form.login')

  await page.typeInto('#username', username)
  await page.typeInto('#password', PASSWORDS[username])
  await paceLogin()
  await page.click('form.login button[type="submit"]')

  await page.waitForSelector('header.bar')
}

const isLoginPage = (page) => page.count('form.login').then((count) => count > 0)

async function uploadThroughUi(page, fixtures, { title, filename, content }) {
  const path = join(fixtures, filename)
  await writeFile(path, content)

  await page.typeInto('#title', title)
  await page.attachFile('input#file', path)
  await page.click('form.upload button[type="submit"]')

  await page.waitForCondition(
    `[...document.querySelectorAll('.materials strong')].some((n) => n.textContent === ${JSON.stringify(title)})`,
    { timeoutMs: 25_000, label: `${filename} to appear in the list` },
  )
}

// ------------------------------------------------------------ scenarios -----

async function checkLoginFlow(page) {
  begin('WE02', '未登录进入登录页；错误密码留在登录页；正确凭证进入材料页')

  await signOut(page)
  await page.goto(BASE)
  await page.waitForSelector('form.login')

  check(await isLoginPage(page), 'the login form was not shown for an anonymous visitor')

  await page.typeInto('#username', 'teacher_a')
  await page.typeInto('#password', 'definitely-the-wrong-password')
  await paceLogin()
  await page.click('form.login button[type="submit"]')

  await page.waitForSelector('form.login .error', { timeoutMs: 15_000 })
  check(await isLoginPage(page), 'a wrong password navigated away from the login page')

  await page.typeInto('#password', PASSWORDS.teacher_a)
  await paceLogin()
  await page.click('form.login button[type="submit"]')
  await page.waitForSelector('header.bar', { timeoutMs: 15_000 })

  const header = await page.textContent('header.bar')
  check(header.includes('teacher_a'), `header ${JSON.stringify(header)} does not show the username`)
  check(header.includes('教师'), `header ${JSON.stringify(header)} does not show the teacher role`)
  check(header.includes('Class A'), `header ${JSON.stringify(header)} does not show the class name`)
}

async function checkSessionRestore(page) {
  begin('WE01', '刷新后由 GET /api/me 恢复登录状态，且全程无 Cookie')

  await signIn(page, 'teacher_a')

  // The credential lives in localStorage and nowhere else: the browser holds no
  // cookie at all, and even the page itself cannot read one.
  const { cookies } = await page.send('Network.getCookies', { urls: [BASE_ORIGIN] })
  check(cookies.length === 0, `the browser holds ${cookies.length} cookie(s): ${JSON.stringify(cookies)}`)
  check((await page.evaluate('document.cookie')) === '', 'document.cookie is not empty')

  const stored = await page.evaluate('localStorage.getItem("campusclaw.access_token")')
  check(typeof stored === 'string' && stored.length > 0, 'the access token was not stored in localStorage')

  await page.reload()
  await page.waitForSelector('header.bar', { timeoutMs: 15_000 })

  const header = await page.textContent('header.bar')
  check(header.includes('teacher_a'), 'the session was not restored after a reload')
  check(!(await isLoginPage(page)), 'a reload with a valid session fell back to the login page')
}

async function checkTeacherUpload(page, fixtures) {
  begin('WE03', '教师上传：表单存在、列表刷新、详情显示提取文本、下载走认证 API')

  await signIn(page, 'teacher_a')

  check((await page.count('input#file')) > 0, 'the teacher has no file input')
  check(
    (await page.attribute('input#file', 'accept')) === '.md,.txt,.pdf,.docx',
    'the file input accept list is not .md,.txt,.pdf,.docx',
  )

  const note = '浏览器验收 Markdown 正文'
  const title = `浏览器上传 ${timestamp()}`

  await uploadThroughUi(page, fixtures, {
    title,
    filename: 'browser.md',
    content: Buffer.from(`${note}\n`, 'utf8'),
  })

  await page.click('.materials li:nth-child(1) button')
  await page.waitForSelector('.content', { timeoutMs: 15_000 })

  const shown = await page.textContent('.content')
  check(shown.includes(note), `detail panel shows ${JSON.stringify(shown)}, want the uploaded text`)

  // The row's actions are named rather than counted: a teacher also gets the
  // index control, and the point of this check is that viewing and downloading
  // are authenticated controls, not how many there are.
  const actions = await page.evaluate(`
    [...document.querySelectorAll('.materials li:nth-child(1) .material-actions button')]
      .map((button) => button.textContent.trim())
  `)
  check(actions.includes('查看'), `the material row has no 查看 control: ${JSON.stringify(actions)}`)
  check(actions.includes('下载'), `the material row has no 下载 control: ${JSON.stringify(actions)}`)

  await page.evaluate(`
    [...document.querySelectorAll('.materials li:nth-child(1) .material-actions button')]
      .find((button) => button.textContent.trim() === '下载')?.click()
  `)
}

async function checkStudentView(page, fixtures) {
  begin('WE04', '学生无上传 UI，localStorage 篡改不改变身份，材料内脚本不执行')

  // The teacher stages a material whose text looks like an injection attempt.
  await signIn(page, 'teacher_a')

  const hostile = '<script>window.__campusclawPwned = true</script>'
  const title = `脚本注入验收 ${timestamp()}`

  await uploadThroughUi(page, fixtures, {
    title,
    filename: 'hostile.md',
    content: Buffer.from(`${hostile}\nplain text tail\n`, 'utf8'),
  })

  await signIn(page, 'student_a1')

  check((await page.count('form.upload')) === 0, 'the student was shown an upload form')

  // Tampering with client-side state must not grant anything.
  await page.evaluate(`
    localStorage.setItem('role', 'teacher');
    localStorage.setItem('class_id', '999');
  `)
  await page.reload()
  await page.waitForSelector('header.bar', { timeoutMs: 15_000 })

  const header = await page.textContent('header.bar')
  check(
    header.includes('学生'),
    `after localStorage tampering the header reads ${JSON.stringify(header)}`,
  )
  check((await page.count('form.upload')) === 0, 'localStorage tampering revealed the upload form')

  // The newest material is the one just uploaded, and it must render as text.
  await page.waitForCondition(
    `document.querySelector('.materials li strong')?.textContent === ${JSON.stringify(title)}`,
    { timeoutMs: 20_000, label: 'the hostile material to be the newest row' },
  )

  await page.click('.materials li:nth-child(1) button')
  await page.waitForSelector('.content', { timeoutMs: 15_000 })

  const shown = await page.textContent('.content')
  check(shown.includes('<script>'), 'the material text was not rendered literally')

  const executed = await page.evaluate('window.__campusclawPwned === true')
  check(!executed, 'script inside the material executed in the page')
}

async function checkLogout(page) {
  begin('WE05', '登出后回到登录页，刷新仍未登录')

  await signIn(page, 'student_a1')

  await page.click('header.bar button')
  await page.waitForSelector('form.login', { timeoutMs: 15_000 })

  await page.reload()
  await page.waitForSelector('form.login', { timeoutMs: 15_000 })

  check(await isLoginPage(page), 'a reload after logout did not return to the login page')
}

async function checkDevOriginGuard() {
  begin('AC32-ORIGIN', 'Vite proxy keeps foreign Origin rejected by Go')
  const response = await fetch(`${BASE}/api/login`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Origin: 'http://evil.example' },
    body: JSON.stringify({ username: 'teacher_a', password: 'wrong' }),
  })
  check(response.status === 403, `foreign Origin returned ${response.status}, want 403`)
}

async function checkRateLimitUi(page) {
  begin('WE06', 'Login 429 shows a generic wait message and stays logged out')
  await signOut(page)
  await page.goto(BASE)
  await page.waitForSelector('form.login')

  const limited = await page.evaluate(`(async () => {
    for (let i = 0; i < 24; i++) {
      const response = await fetch('/api/login', {
        method: 'POST',
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username: 'no_such_user', password: 'wrong' }),
      })
      if (response.status === 429) return true
    }
    return false
  })()`)
  check(limited, 'login burst did not reach 429')
  if (!limited) return

  await page.typeInto('#username', 'teacher_a')
  await page.typeInto('#password', PASSWORDS.teacher_a)
  await page.click('form.login button[type="submit"]')
  await page.waitForSelector('form.login .error', { timeoutMs: 15_000 })
  const message = await page.textContent('form.login .error')
  check(message.includes('\u8bf7\u6c42\u8fc7\u4e8e\u9891\u7e41'), `429 message = ${JSON.stringify(message)}`)
  check(await isLoginPage(page), 'rate-limited login left the login page')
}

function checkSingleOrigin(page) {
  begin(BASE_ORIGIN === 'http://localhost:5173' ? 'AC32' : 'AC23', `Browser requests stay on ${BASE_ORIGIN}`)

  const requests = page.requests.splice(0, page.requests.length)
  const outside = requests.filter((url) => !url.startsWith(BASE_ORIGIN))

  check(
    outside.length === 0,
    `${outside.length} request(s) left the published origin, e.g. ${outside[0] ?? ''}`,
  )
  check(requests.length > 0, 'no requests were recorded, so the origin assertion is vacuous')
}

// ---------------------------------------------------------- retrieval ------

/** selectOption drives a <select> the way a user's change event does. */
async function selectOption(page, selector, value) {
  await page.evaluate(`(() => {
    const element = document.querySelector(${JSON.stringify(selector)})
    const setter = Object.getOwnPropertyDescriptor(window.HTMLSelectElement.prototype, 'value').set
    setter.call(element, ${JSON.stringify(value)})
    element.dispatchEvent(new window.Event('change', { bubbles: true }))
  })()`)
}

/**
 * setInputValue changes an input through the native setter and a bubbling
 * input event, which is what React listens for. Assigning .value directly would
 * update the DOM but leave the component's state — and therefore the next
 * request — unchanged.
 */
async function setInputValue(page, selector, value) {
  await page.evaluate(`(() => {
    const element = document.querySelector(${JSON.stringify(selector)})
    const setter = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value').set
    setter.call(element, ${JSON.stringify(value)})
    element.dispatchEvent(new window.Event('input', { bubbles: true }))
  })()`)
}

/**
 * The material the retrieval scenarios search for. It contains the term the
 * keyword path matches and the concept the vector path matches, so one fixture
 * exercises both.
 */
const RETRIEVAL_NOTE = '向量检索给出了可以核对的出处。'
const RETRIEVAL_TERM = '向量检索'

async function checkTeacherIndexControls(page, fixtures) {
  begin('WE13', '教师可见切成策略与索引状态，重建后仍可检索')

  await signIn(page, 'teacher_a')

  const title = `检索讲义 ${timestamp()}`
  await uploadThroughUi(page, fixtures, {
    title,
    filename: 'retrieval.md',
    content: Buffer.from(`${RETRIEVAL_TERM}：${RETRIEVAL_NOTE}\n`, 'utf8'),
  })

  // The newest row is the fixture just uploaded.
  await page.click('.materials li:nth-child(1) .index-panel button')
  await page.waitForSelector('.index-status', { timeoutMs: 20_000 })

  const status = await page.textContent('.index-status')
  check(status.includes('已就绪'), `index status reads ${JSON.stringify(status)}, want 已就绪`)

  check((await page.count(`#reindex-${await newestMaterialID(page)}-strategy`)) > 0,
    'the rebuild control does not offer a split strategy')

  // Rebuilding with an explicit strategy must leave the material searchable.
  const strategyId = await page.evaluate(
    'document.querySelector(".index-panel select[id$=\'-strategy\']")?.id ?? ""',
  )
  check(strategyId !== '', 'the rebuild form has no strategy select')
  if (strategyId !== '') {
    await selectOption(page, `#${strategyId}`, 'hierarchy')
    await page.click('.index-body button:not(.secondary)')
    // The panel already reported 已就绪 before the rebuild, so the wait has to
    // be for the new strategy, not for readiness.
    await page.waitForCondition(
      'document.querySelector(".index-status")?.textContent.includes("hierarchy")',
      { timeoutMs: 30_000, label: 'the rebuilt index to report the hierarchy strategy' },
    )
    check(
      (await page.textContent('.index-status')).includes('已就绪'),
      'the rebuilt index is not ready',
    )
  }
}

/** newestMaterialID reads the id the newest row's index panel is keyed by. */
async function newestMaterialID(page) {
  return page.evaluate(`
    document.querySelector('.index-panel select[id$="-strategy"]')?.id.replace(/^reindex-/, '').replace(/-strategy$/, '') ?? ''
  `)
}

async function checkRetrievalSearch(page) {
  begin('WE10', '学生可用三种方式检索，出处可打开，空输入与无依据分别提示')

  await signIn(page, 'student_a1')

  check((await page.count('.index-panel')) === 0, 'the student was shown the teacher index controls')

  await page.typeInto('#search-query', RETRIEVAL_TERM)
  await page.click('form.search button[type="submit"]')
  await page.waitForSelector('.hit', { timeoutMs: 30_000 })

  const hit = await page.textContent('.hit')
  check(hit.includes(RETRIEVAL_TERM), `the first hit does not contain the query term: ${JSON.stringify(hit)}`)
  check(/切片 \d+/.test(hit), `the hit does not show its slice number: ${JSON.stringify(hit)}`)
  check(/字符 \d+–\d+/.test(hit), `the hit does not show its character range: ${JSON.stringify(hit)}`)

  // Searching by meaning, not by wording, through the vector mode.
  await selectOption(page, '#search-mode', 'vector')
  await page.click('form.search button[type="submit"]')
  await page.waitForCondition(
    'document.querySelectorAll(".hit").length > 0',
    { timeoutMs: 30_000, label: 'a vector search result' },
  )
  check((await page.count('.hit')) > 0, 'a vector search returned nothing')

  // Nothing on the page may be a raw vector component.
  const body = await page.textContent('body')
  check(!/\[(?:-?\d+\.\d+\s*,){3,}/.test(body), 'the page rendered something that looks like a raw vector')

  // The top hit is whichever material ranks first, so the check is that the
  // opened detail is the hit's own material — not that it is this test's
  // fixture, which other material in the class may outrank.
  const hitTitle = await page.evaluate(
    'document.querySelector(".hit .hit-head strong")?.textContent ?? ""',
  )
  await page.click('.hit .hit-actions button')
  await page.waitForSelector('.detail-card', { timeoutMs: 20_000 })

  const openedTitle = await page.evaluate(
    'document.querySelector(".detail-card h2")?.textContent ?? ""',
  )
  check(hitTitle !== '' && openedTitle === hitTitle,
    `opening the hit showed ${JSON.stringify(openedTitle)}, want ${JSON.stringify(hitTitle)}`)
  check((await page.evaluate(
    'document.querySelector(".detail-card .content")?.textContent.length ?? 0',
  )) > 0, 'the opened material shows no text')

  // An empty query is a fixable mistake, not an empty result.
  await setInputValue(page, '#search-query', '')
  await page.click('form.search button[type="submit"]')
  await page.waitForSelector('.retrieval-card .error', { timeoutMs: 15_000 })
  check(
    (await page.textContent('.retrieval-card .error')).includes('请输入'),
    'an empty query did not ask for input',
  )

  // An unrelated query is an empty success, not an error.
  await page.typeInto('#search-query', '天气预报和比分')
  await page.click('form.search button[type="submit"]')
  await page.waitForSelector('.retrieval-card .notice', { timeoutMs: 30_000 })
  check(
    (await page.textContent('.retrieval-card .notice')).includes('资料中未找到相关内容'),
    'an unrelated query did not show the fixed notice',
  )
}

async function checkAnswerCitations(page) {
  begin('WE12', '回答中的编号与出处列表一一对应，可选中对应出处')

  await signIn(page, 'student_a1')

  await page.typeInto('#ask-question', `${RETRIEVAL_TERM}说了什么`)
  await page.click('form.ask button[type="submit"]')
  await page.waitForSelector('.answer-block', { timeoutMs: 40_000 })

  const citations = await page.count('.citation')
  check(citations > 0, 'an answer supported by the material came back with no citations')

  const markers = await page.count('.citation-link')
  check(markers > 0, 'no citation marker in the answer was linked to its source')

  if (markers > 0) {
    await page.click('.citation-link')
    await page.waitForSelector('.citation-active', { timeoutMs: 10_000 })

    const active = await page.textContent('.citation-active')
    check(active.includes('[1]'), `the first marker selected ${JSON.stringify(active)}`)
  }

  // An unsupported question is answered without sources.
  await page.typeInto('#ask-question', '这座城市的天气如何')
  await page.click('form.ask button[type="submit"]')
  await page.waitForCondition(
    'document.querySelector(".answer")?.textContent.includes("资料中未找到相关内容")',
    { timeoutMs: 40_000, label: 'the fixed no-evidence answer' },
  )
  check(
    (await page.count('.citation-active')) === 0,
    'an unsupported question left a citation selected',
  )
}

async function checkNarrowViewport(page) {
  begin('WE11', '390px 视口下检索与问答可用且不产生横向滚动')

  await page.send('Emulation.setDeviceMetricsOverride', {
    width: 390,
    height: 844,
    deviceScaleFactor: 1,
    mobile: true,
  })

  try {
    await signIn(page, 'student_a1')
    await page.typeInto('#search-query', RETRIEVAL_TERM)
    await page.click('form.search button[type="submit"]')
    await page.waitForSelector('.hit', { timeoutMs: 30_000 })

    const layout = await page.evaluate(`({
      scrollWidth: document.documentElement.scrollWidth,
      innerWidth: window.innerWidth,
      buttonVisible: (() => {
        const button = document.querySelector('form.search button[type="submit"]')
        return button !== null && button.getBoundingClientRect().width > 0
      })(),
    })`)

    check(layout.scrollWidth <= layout.innerWidth + 1,
      `the page scrolls horizontally: ${layout.scrollWidth} > ${layout.innerWidth}`)
    check(layout.buttonVisible, 'the search button is not visible at 390px')
  } finally {
    await page.send('Emulation.clearDeviceMetricsOverride')
  }
}

// ---------------------------------------------------------------- main ------

async function main() {
  const fixtures = await mkdtemp(join(tmpdir(), 'campusclaw-fixtures-'))
  const browser = await Browser.launch()

  try {
    const page = await browser.newPage()

    await checkLoginFlow(page)
    await checkSessionRestore(page)
    await checkTeacherUpload(page, fixtures)
    await checkStudentView(page, fixtures)
    // Retrieval runs before the rate-limit case, which deliberately exhausts the
    // login limiter and would leave every later sign-in refused.
    await checkTeacherIndexControls(page, fixtures)
    await checkRetrievalSearch(page)
    await checkAnswerCitations(page)
    await checkNarrowViewport(page)
    await checkLogout(page)
    if (BASE_ORIGIN === 'http://localhost:5173') {
      await checkDevOriginGuard()
    }
    await checkRateLimitUi(page)
    checkSingleOrigin(page)

    for (const error of page.pageErrors) {
      check(false, `uncaught page error: ${error}`)
    }
  } finally {
    await browser.close()
    await rm(fixtures, { recursive: true, force: true }).catch(() => {})
  }

  // Set the exit code rather than calling process.exit(): exiting immediately
  // truncates buffered stdout, which would swallow the report when the output
  // is redirected to a file.
  process.exitCode = report() === 0 ? 0 : 1
}

await main()
