import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { fileURLToPath } from 'node:url'
import ts from 'typescript'

const source = await readFile(fileURLToPath(new URL('../src/api.ts', import.meta.url)), 'utf8')
const compiled = ts.transpileModule(source, {
  compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 },
}).outputText

const TOKEN_KEY = 'campusclaw.access_token'

const user = { id: '1', username: 'teacher_a', role: 'teacher', class_id: '1', class_name: 'Class A' }
const authBody = (token) => ({ user, token, expires_at: '2030-01-01T00:00:00Z' })
const json = (body, status = 200) => new Response(JSON.stringify(body), {
  status, headers: { 'Content-Type': 'application/json' },
})
const unauthorized = () => json({ error: { code: 'unauthorized', message: 'Unauthorized' } }, 401)

/**
 * Installs a fresh in-memory localStorage, returning the backing map so a test
 * can inspect exactly what the module wrote.
 */
function installStorage() {
  const entries = new Map()
  globalThis.localStorage = {
    getItem: (key) => (entries.has(key) ? entries.get(key) : null),
    setItem: (key, value) => { entries.set(key, String(value)) },
    removeItem: (key) => { entries.delete(key) },
    clear: () => { entries.clear() },
  }
  return entries
}

async function apiModule() {
  return import('data:text/javascript;base64,' + Buffer.from(compiled).toString('base64') + '#' + Math.random())
}

/** Reads one header from init, whether it was passed as a Headers or a plain object. */
const headerOf = (init, name) => new Headers(init?.headers).get(name)

test('login stores the token and business requests carry it as a Bearer header', async (t) => {
  const storage = installStorage()
  const oldFetch = globalThis.fetch
  const calls = []
  globalThis.fetch = async (path, init) => {
    calls.push({ path, init })
    if (path === '/api/login') return json(authBody('access-a'))
    if (path === '/api/materials') return json({ materials: [] })
    throw Error('unexpected request: ' + path)
  }
  t.after(() => { globalThis.fetch = oldFetch })

  const api = await apiModule()
  assert.equal((await api.login('teacher_a', 'password')).username, 'teacher_a')

  assert.equal(storage.get(TOKEN_KEY), 'access-a')
  assert.deepEqual([...storage.keys()], [TOKEN_KEY])

  assert.deepEqual(await api.listMaterials(), [])
  assert.equal(headerOf(calls[0].init, 'Authorization'), null)
  assert.equal(headerOf(calls[1].init, 'Authorization'), 'Bearer access-a')

  // The token travels in the header only: not in the URL, and not anywhere the
  // page renders.
  assert.equal(calls[1].path, '/api/materials')
  assert.ok(!calls[1].path.includes('access-a'))
})

test('a stored token restores the session without any refresh call', async (t) => {
  installStorage().set(TOKEN_KEY, 'stored-token')
  const oldFetch = globalThis.fetch
  const calls = []
  globalThis.fetch = async (path, init) => {
    calls.push([path, init.headers?.get('Authorization') ?? null])
    if (path === '/api/me') return json(user)
    if (path === '/api/materials') return json({ materials: [] })
    throw Error('unexpected request: ' + path)
  }
  t.after(() => { globalThis.fetch = oldFetch })

  const api = await apiModule()
  assert.equal((await api.restoreSession())?.username, 'teacher_a')
  assert.deepEqual(await api.listMaterials(), [])
  assert.deepEqual(calls, [
    ['/api/me', 'Bearer stored-token'],
    ['/api/materials', 'Bearer stored-token'],
  ])
})

test('no stored token means no request at all', async (t) => {
  installStorage()
  const oldFetch = globalThis.fetch
  let calls = 0
  globalThis.fetch = async () => { calls++; return unauthorized() }
  t.after(() => { globalThis.fetch = oldFetch })

  const api = await apiModule()
  assert.equal(await api.restoreSession(), null)
  assert.equal(calls, 0)
  assert.equal(await api.getCurrentUser(), null)
  assert.equal(calls, 0)
})

test('a rejected token is dropped and the request is not retried', async (t) => {
  const storage = installStorage()
  storage.set(TOKEN_KEY, 'dead-token')
  const oldFetch = globalThis.fetch
  const calls = []
  globalThis.fetch = async (path) => {
    calls.push(path)
    return unauthorized()
  }
  t.after(() => { globalThis.fetch = oldFetch })

  const api = await apiModule()
  assert.equal(await api.restoreSession(), null)
  assert.deepEqual(calls, ['/api/me'])
  assert.equal(storage.has(TOKEN_KEY), false)

  calls.length = 0
  storage.set(TOKEN_KEY, 'dead-token')
  await assert.rejects(api.listMaterials(), (error) => error.status === 401)
  assert.deepEqual(calls, ['/api/materials'])
  assert.equal(storage.has(TOKEN_KEY), false)
})

test('transport and server failures keep the stored token', async (t) => {
  const storage = installStorage()
  const oldFetch = globalThis.fetch
  t.after(() => { globalThis.fetch = oldFetch })

  const api = await apiModule()

  storage.set(TOKEN_KEY, 'still-good')
  globalThis.fetch = async () => json({ error: { code: 'internal_error', message: 'server down' } }, 500)
  await assert.rejects(api.restoreSession(), (error) => error instanceof api.ApiError && error.status === 500)
  assert.equal(storage.get(TOKEN_KEY), 'still-good')

  globalThis.fetch = async () => { throw Error('network down') }
  await assert.rejects(api.restoreSession(), /network down/)
  assert.equal(storage.get(TOKEN_KEY), 'still-good')
})

test('logout clears the stored token, and a failed logout keeps it', async (t) => {
  const storage = installStorage()
  const oldFetch = globalThis.fetch
  t.after(() => { globalThis.fetch = oldFetch })

  const api = await apiModule()

  storage.set(TOKEN_KEY, 'live-token')
  globalThis.fetch = async (path, init) => {
    assert.equal(headerOf(init, 'Authorization'), 'Bearer live-token')
    return new Response(null, { status: 204 })
  }
  await api.logout()
  assert.equal(storage.has(TOKEN_KEY), false)

  storage.set(TOKEN_KEY, 'live-token')
  globalThis.fetch = async () => json({ error: { code: 'internal_error', message: 'nope' } }, 500)
  await assert.rejects(api.logout(), (error) => error.status === 500)
  assert.equal(storage.get(TOKEN_KEY), 'live-token')
})

test('download uses authenticated fetch and revokes its temporary object URL', async (t) => {
  installStorage().set(TOKEN_KEY, 'access-c')
  const oldFetch = globalThis.fetch
  const oldDocument = globalThis.document
  const oldWindow = globalThis.window
  const oldCreate = URL.createObjectURL
  const oldRevoke = URL.revokeObjectURL
  const observed = { clicked: false, revoked: false }
  globalThis.fetch = async (path, init) => {
    assert.equal(path, '/api/materials/7/file')
    assert.equal(headerOf(init, 'Authorization'), 'Bearer access-c')
    return new Response(new Uint8Array([0, 1, 2, 255]), {
      headers: { 'Content-Disposition': "attachment; filename*=UTF-8''notes%20one.pdf" },
    })
  }
  URL.createObjectURL = () => 'blob:temporary'
  URL.revokeObjectURL = (url) => { observed.revoked = url === 'blob:temporary' }
  globalThis.document = {
    createElement: () => ({
      set href(value) { observed.href = value },
      set download(value) { observed.filename = value },
      click() { observed.clicked = true },
      remove() {},
    }),
    body: { append() {} },
  }
  globalThis.window = { setTimeout: (fn) => fn() }
  t.after(() => {
    globalThis.fetch = oldFetch
    globalThis.document = oldDocument
    globalThis.window = oldWindow
    URL.createObjectURL = oldCreate
    URL.revokeObjectURL = oldRevoke
  })

  const api = await apiModule()
  await api.downloadMaterial('7', 'fallback.pdf')
  assert.deepEqual(observed, {
    clicked: true, revoked: true, href: 'blob:temporary', filename: 'notes one.pdf',
  })
})

test('download errors do not create a URL and quoted attachment names stay intact', async (t) => {
  installStorage().set(TOKEN_KEY, 'access-d')
  const oldFetch = globalThis.fetch
  const oldDocument = globalThis.document
  const oldWindow = globalThis.window
  const oldCreate = URL.createObjectURL
  const oldRevoke = URL.revokeObjectURL
  let created = 0
  let filename = ''
  let attempts = 0
  globalThis.fetch = async () => {
    attempts++
    if (attempts === 1) return json({ error: { code: 'not_found', message: 'not found' } }, 404)
    return new Response(new Uint8Array([65, 66, 67]), {
      headers: { 'Content-Disposition': 'attachment; filename="notes;final.txt"' },
    })
  }
  URL.createObjectURL = () => { created++; return 'blob:temporary' }
  URL.revokeObjectURL = () => {}
  globalThis.document = {
    createElement: () => ({
      set href(_) {},
      set download(value) { filename = value },
      click() {},
      remove() {},
    }),
    body: { append() {} },
  }
  globalThis.window = { setTimeout: (fn) => fn() }
  t.after(() => {
    globalThis.fetch = oldFetch
    globalThis.document = oldDocument
    globalThis.window = oldWindow
    URL.createObjectURL = oldCreate
    URL.revokeObjectURL = oldRevoke
  })

  const api = await apiModule()
  await assert.rejects(api.downloadMaterial('9', 'fallback.txt'), (error) => error instanceof api.ApiError && error.status === 404)
  assert.equal(created, 0)

  await api.downloadMaterial('9', 'fallback.txt')
  assert.equal(created, 1)
  assert.equal(filename, 'notes;final.txt')
})
