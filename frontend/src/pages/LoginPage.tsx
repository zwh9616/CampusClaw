import { useState, type FormEvent } from 'react'
import * as api from '../api'
import type { User } from '../api'

interface Props {
  onAuthenticated: (user: User) => void
}

export default function LoginPage({ onAuthenticated }: Props) {
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  async function handleSubmit(event: FormEvent) {
    event.preventDefault()

    setBusy(true)
    setError(null)

    try {
      onAuthenticated(await api.login(username, password))
    } catch (failure) {
      // The server answers identically for a wrong password and an unknown
      // username, so the message here does not disclose which happened.
      if (failure instanceof api.ApiError) {
        if (failure.status === 429) {
          setError("请求过于频繁，请稍后重试。")
        } else {
          setError(failure.status === 401 ? '用户名或密码不正确。' : failure.message)
        }
      } else {
        setError('无法连接到服务器，请重试。')
      }
    } finally {
      setBusy(false)
    }
  }

  return (
    <main className="app">
      <h1>CampusClaw</h1>
      <p className="subtitle">使用学校发放的账号登录。</p>

      <form className="card login" onSubmit={handleSubmit}>
        <label htmlFor="username">用户名</label>
        <input
          id="username"
          name="username"
          autoComplete="username"
          value={username}
          onChange={(event) => setUsername(event.target.value)}
          required
        />

        <label htmlFor="password">密码</label>
        <input
          id="password"
          name="password"
          type="password"
          autoComplete="current-password"
          value={password}
          onChange={(event) => setPassword(event.target.value)}
          required
        />

        {error !== null && (
          <p className="error" role="alert">
            {error}
          </p>
        )}

        <button type="submit" disabled={busy}>
          {busy ? '登录中…' : '登录'}
        </button>
      </form>
    </main>
  )
}
