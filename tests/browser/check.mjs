// Browser end-to-end checks for WE01-WE06 and AC23/AC32.
//
// The scenarios share a single browser page and separate themselves by clearing
// cookies and reloading, rather than opening a fresh browser context each time.
// That keeps the run stable and mirrors what a user actually does: log out, log
// in as somebody else, reload.
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

/** signOut clears the session cookie and reloads onto the login page. */
async function signOut(page) {
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
  begin('WE01', '刷新后由 GET /api/me 恢复登录状态')

  await signIn(page, 'teacher_a')
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

  const href = await page.attribute('.materials li:nth-child(1) a', 'href')
  check(
    /^\/api\/materials\/\d+\/file$/.test(href ?? ''),
    `download link ${JSON.stringify(href)} does not point at the authenticated API`,
  )
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
