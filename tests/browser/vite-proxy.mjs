// Smoke check for the Vite ? Nginx ? API proxy path.
// Start Vite with API_PROXY_TARGET pointing at the isolated Nginx stack.
import assert from 'node:assert/strict'

const origin = 'http://localhost:5173'
const password = process.env.TEACHER_A_PASSWORD
if (!password) throw Error('TEACHER_A_PASSWORD is required')

const login = await fetch(origin + '/api/login', {
  method: 'POST',
  headers: { Origin: origin, 'Content-Type': 'application/json' },
  body: JSON.stringify({ username: 'teacher_a', password }),
})
assert.equal(login.status, 200, 'same-origin login through Vite')
const session = await login.json()
assert.ok(session.token, 'login returned no access token')
assert.equal(login.headers.get('set-cookie'), null, 'login set a cookie')
assert.equal(login.headers.get('cache-control'), 'no-store')

const me = (headers) => fetch(origin + '/api/me', { headers })

assert.equal((await me({ Authorization: 'Bearer ' + session.token })).status, 200,
  'Vite did not forward the Authorization header')

// A cookie riding along with a valid Bearer header is simply ignored, and on its
// own it is not a credential at all.
assert.equal((await me({
  Authorization: 'Bearer ' + session.token,
  Cookie: 'campusclaw_session=' + session.token,
})).status, 200, 'a stray cookie changed the outcome of a Bearer request')

const cookieOnly = await me({
  Cookie: 'campusclaw_session=' + session.token + '; campusclaw_refresh=' + session.token,
})
assert.equal(cookieOnly.status, 401, 'a cookie alone authenticated the request')

const foreign = await fetch(origin + '/api/login', {
  method: 'POST',
  headers: { Origin: 'http://evil.example', 'Content-Type': 'application/json' },
  body: JSON.stringify({ username: 'teacher_a', password: 'wrong' }),
})
assert.equal(foreign.status, 403, 'foreign Origin remains rejected')
console.log('Vite proxy: Authorization forwarding, cookie-free transport and origin checks passed')
