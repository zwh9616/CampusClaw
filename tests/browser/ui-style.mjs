// Checks the built frontend's visual contract without requiring seeded accounts.
// Run `npm run build` in frontend/ first, then `node tests/browser/ui-style.mjs`.
import { createServer } from 'node:http'
import { readFile, writeFile } from 'node:fs/promises'
import { dirname, extname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { setTimeout as delay } from 'node:timers/promises'
import { Browser } from './cdp.mjs'

const root = join(dirname(fileURLToPath(import.meta.url)), '../..')
const dist = join(root, 'frontend/dist')
const previews = join(root, 'design-preview/beida-red')
const materials = [
  {
    id: '1', class_id: '1', uploaded_by: '1', title: '第一单元课堂讲义',
    original_filename: '课程讲义.pdf', content_type: 'application/pdf',
    created_at: '2026-09-29T01:30:00Z',
  },
  {
    id: '2', class_id: '1', uploaded_by: '1', title: '阅读与思考：课后练习',
    original_filename: '课后练习.docx',
    content_type: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
    created_at: '2026-09-28T08:20:00Z',
  },
]

let role = 'anonymous'
const server = createServer(async (request, response) => {
  const path = new URL(request.url, 'http://localhost').pathname
  if (path === '/api/me') {
    response.writeHead(role === 'anonymous' ? 401 : 200, { 'Content-Type': 'application/json' })
    response.end(JSON.stringify(role === 'anonymous'
      ? { error: { code: 'unauthorized', message: 'Unauthorized' } }
      : { id: '1', username: `${role}_a`, role, class_id: '1', class_name: '高一一班' }))
    return
  }
  if (path === '/api/login') {
    response.writeHead(401, { 'Content-Type': 'application/json' })
    response.end(JSON.stringify({ error: { code: 'invalid_credentials', message: 'Invalid credentials' } }))
    return
  }
  if (path === '/api/materials') {
    response.writeHead(200, { 'Content-Type': 'application/json' })
    response.end(JSON.stringify({ materials }))
    return
  }
  if (path === '/api/materials/1') {
    response.writeHead(200, { 'Content-Type': 'application/json' })
    response.end(JSON.stringify({ material: materials[0], content: '<script>window.evil = true</script>' }))
    return
  }
  const file = path.startsWith('/assets/') ? join(dist, path.slice(1)) : join(dist, 'index.html')
  try {
    const bytes = await readFile(file)
    const type = extname(file) === '.js' ? 'text/javascript'
      : extname(file) === '.css' ? 'text/css' : 'text/html'
    response.writeHead(200, { 'Content-Type': type })
    response.end(bytes)
  } catch {
    response.writeHead(404)
    response.end()
  }
})

function assert(condition, message) {
  if (!condition) throw new Error(message)
}

async function screenshot(page, name) {
  await delay(250)
  const image = await page.send('Page.captureScreenshot', { format: 'png' })
  await writeFile(join(previews, name), Buffer.from(image.data, 'base64'))
}

async function inspectViewport(width, base, port) {
  const browser = await Browser.launch({ port })
  try {
    const page = await browser.newPage()
    await page.send('Emulation.setDeviceMetricsOverride', {
      width, height: 900, deviceScaleFactor: 1, mobile: false,
    })

    role = 'anonymous'
    await page.goto(base)
    await page.waitForSelector('form.login')
    const login = await page.evaluate(`(() => {
      const story = document.querySelector('.login-story').getBoundingClientRect()
      const form = document.querySelector('form.login').getBoundingClientRect()
      const input = document.querySelector('#username')
      input.focus()
      return {
        brand: getComputedStyle(document.documentElement).getPropertyValue('--brand').trim(),
        mark: document.querySelector('.brand-mark')?.textContent,
        horizontal: story.right <= form.left,
        stacked: story.bottom <= form.top,
        overflow: document.documentElement.scrollWidth > innerWidth,
        focusVisible: input.matches(':focus-visible'),
        outline: getComputedStyle(input).outlineStyle,
      }
    })()`)
    assert(login.brand.toLowerCase() === '#94070a', `login ${width}: wrong brand color`)
    assert(login.mark === 'CC', `login ${width}: missing shared brand`)
    assert(width === 390 ? login.stacked : login.horizontal, `login ${width}: wrong layout`)
    assert(!login.overflow, `login ${width}: horizontal overflow`)
    assert(login.focusVisible && login.outline !== 'none', `login ${width}: focus not visible`)
    await screenshot(page, width === 390 ? 'implemented-login-mobile.png' : 'implemented-login.png')
    await page.typeInto('#username', 'unknown')
    await page.typeInto('#password', 'wrong')
    await page.click('form.login button[type="submit"]')
    await page.waitForSelector('form.login .error')
    assert((await page.textContent('form.login .error')).includes('用户名或密码不正确'),
      `login ${width}: wrong credentials message missing`)
    console.log(`login ${width}px PASS`)

    role = 'teacher'
    await page.reload()
    await page.waitForSelector('.materials li')
    const teacher = await page.evaluate(`(() => {
      const list = document.querySelector('.materials-card').getBoundingClientRect()
      const upload = document.querySelector('.upload-card').getBoundingClientRect()
      const button = document.querySelector('.materials button')
      button.focus()
      return {
        mark: document.querySelector('.brand-mark')?.textContent,
        upload: Boolean(document.querySelector('form.upload')),
        rows: document.querySelectorAll('.materials li').length,
        download: document.querySelector('.materials li a')?.getAttribute('href'),
        horizontal: list.right <= upload.left,
        stacked: list.bottom <= upload.top,
        overflow: document.documentElement.scrollWidth > innerWidth,
        focusVisible: button.matches(':focus-visible'),
        outline: getComputedStyle(button).outlineStyle,
      }
    })()`)
    assert(teacher.mark === 'CC' && teacher.upload && teacher.rows === 2 && teacher.download === '/api/materials/1/file', `teacher ${width}: missing controls`)
    assert(width === 390 ? teacher.stacked : teacher.horizontal, `teacher ${width}: wrong layout`)
    assert(!teacher.overflow, `teacher ${width}: horizontal overflow`)
    assert(teacher.focusVisible && teacher.outline !== 'none', `teacher ${width}: focus not visible`)
    await screenshot(page, width === 390 ? 'implemented-materials-mobile.png' : 'implemented-materials.png')

    await page.click('.materials li:first-child button')
    await page.waitForSelector('.content')
    const safeText = await page.evaluate(`({
      text: document.querySelector('.content').textContent,
      executed: Boolean(window.evil),
    })`)
    assert(safeText.text.includes('<script>') && !safeText.executed, `teacher ${width}: detail text was unsafe`)
    console.log(`teacher ${width}px PASS`)

    role = 'student'
    await page.reload()
    await page.waitForSelector('.materials li')
    const student = await page.evaluate(`({
      upload: Boolean(document.querySelector('form.upload')),
      rows: document.querySelectorAll('.materials li').length,
      download: document.querySelector('.materials li a')?.getAttribute('href'),
      overflow: document.documentElement.scrollWidth > innerWidth,
    })`)
    assert(!student.upload && student.rows === 2 && student.download === '/api/materials/1/file' && !student.overflow, `student ${width}: wrong controls or overflow`)
    await screenshot(page, width === 390 ? 'implemented-student-mobile.png' : 'implemented-student.png')
    console.log(`student ${width}px PASS`)

    const colors = await page.evaluate(`(() => {
      const error = document.createElement('p')
      const notice = document.createElement('p')
      error.className = 'error'
      notice.className = 'notice'
      document.body.append(error, notice)
      const result = {
        error: getComputedStyle(error).color,
        notice: getComputedStyle(notice).color,
        brand: getComputedStyle(document.querySelector('.brand-mark')).backgroundColor,
      }
      error.remove()
      notice.remove()
      return result
    })()`)
    assert(colors.error !== colors.notice && colors.error !== colors.brand && colors.notice !== colors.brand,
      `status ${width}: colors are not distinct`)
    assert(page.pageErrors.length === 0, `${width}: browser errors: ${page.pageErrors.join('; ')}`)
    console.log(`status/focus ${width}px PASS`)
  } finally {
    await browser.close()
  }
}

await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve))
try {
  const base = `http://127.0.0.1:${server.address().port}`
  await inspectViewport(1280, base, 9363)
  await inspectViewport(390, base, 9364)
} finally {
  await new Promise((resolve) => server.close(resolve))
}
