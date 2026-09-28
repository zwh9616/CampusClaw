import { useCallback, useEffect, useState } from 'react'
import * as api from './api'
import type { User } from './api'
import LoginPage from './pages/LoginPage'
import MaterialsPage from './pages/MaterialsPage'

/**
 * Session is the whole client-side identity.
 *
 * It lives in memory only. There is no token, role or class kept in
 * localStorage, and the server re-resolves identity from the session cookie on
 * every request, so nothing here can be edited into a privilege.
 */
type Session =
  | { status: 'loading' }
  | { status: 'unreachable' }
  | { status: 'anonymous' }
  | { status: 'authenticated'; user: User }

export default function App() {
  const [session, setSession] = useState<Session>({ status: 'loading' })

  const restore = useCallback(async () => {
    setSession({ status: 'loading' })

    try {
      const user = await api.getCurrentUser()
      setSession(user === null ? { status: 'anonymous' } : { status: 'authenticated', user })
    } catch {
      // A network or 5xx failure is shown as such and is retryable. Treating it
      // as "logged out" would silently discard a working session.
      setSession({ status: 'unreachable' })
    }
  }, [])

  useEffect(() => {
    void restore()
  }, [restore])

  if (session.status === 'loading') {
    return (
      <main className="app">
        <p className="muted">正在恢复会话…</p>
      </main>
    )
  }

  if (session.status === 'unreachable') {
    return (
      <main className="app">
        <h1>CampusClaw</h1>
        <p className="error" role="alert">
          无法连接到服务器。
        </p>
        <button type="button" onClick={() => void restore()}>
          重试
        </button>
      </main>
    )
  }

  if (session.status === 'anonymous') {
    return (
      <LoginPage
        onAuthenticated={(user) => setSession({ status: 'authenticated', user })}
      />
    )
  }

  return (
    <MaterialsPage
      user={session.user}
      onSignOut={() => setSession({ status: 'anonymous' })}
      onUnauthorized={() => setSession({ status: 'anonymous' })}
    />
  )
}
