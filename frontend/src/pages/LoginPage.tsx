import { useState, type FormEvent } from 'react'
import * as api from '../api'
import type { User } from '../api'
import Brand from '../components/Brand'

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
          setError('请求过于频繁，请稍后重试。')
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
    <main className="login-page">
      <section className="login-story" aria-label="平台介绍">
        <Brand />
        <div className="login-story-copy">
          <span className="decorative-rule" aria-hidden="true" />
          <p className="eyebrow">CAMPUS LEARNING SPACE</p>
          <h1>让每一份教学资料，<br />都有清晰的归处。</h1>
          <p className="story-summary">课程资料集中保存，师生随时查阅。把注意力留给学习本身。</p>
        </div>
        <p className="story-foot">CampusClaw · 校园教学材料平台</p>
      </section>

      <div className="login-side">
        <form className="login login-card" onSubmit={handleSubmit}>
          <p className="eyebrow">WELCOME BACK</p>
          <h2>登录平台</h2>
          <p className="muted">使用学校发放的账号登录。</p>

          <div className="field">
            <label htmlFor="username">用户名</label>
            <input
              id="username"
              name="username"
              autoComplete="username"
              value={username}
              onChange={(event) => setUsername(event.target.value)}
              required
            />
          </div>

          <div className="field">
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
          </div>

          {error !== null && (
            <p className="error" role="alert">
              {error}
            </p>
          )}

          <button className="button login-submit" type="submit" disabled={busy}>
            {busy ? '登录中…' : '登录'}
          </button>
        </form>
      </div>
    </main>
  )
}
