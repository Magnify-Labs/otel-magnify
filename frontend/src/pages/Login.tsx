import { useEffect, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { authAPI, meAPI, type AuthMethod } from '../api/client'
import { startClientSession } from '../api/session'
import { useStore } from '../store'

const PASSWORD_METHOD: AuthMethod = {
  id: 'password',
  type: 'password',
  display_name: 'Email + password',
  login_url: '/api/auth/login',
}

export default function Login() {
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const setMe = useStore((s) => s.setMe)
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  // Default to the password-only list so the page stays usable if the
  // methods endpoint fails. Overwritten on mount with the server's answer.
  const [methods, setMethods] = useState<AuthMethod[]>([PASSWORD_METHOD])

  useEffect(() => {
    let cancelled = false
    authAPI
      .getMethods()
      .then((m) => {
        if (!cancelled) setMethods(m)
      })
      .catch(() => {
        // Fall back silently to the password-only default already in state.
      })
    return () => {
      cancelled = true
    }
  }, [])

  const hasPassword = methods.some((m) => m.type === 'password')
  const ssoMethods = methods.filter((m) => m.type === 'sso')
  const expired = searchParams.has('expired')

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')
    setLoading(true)
    try {
      await authAPI.login(email, password)
      startClientSession()
      // AppShell's boot effect already ran with no token; hydrate `me` here
      // so the SPA navigate to '/' lands on a screen with the user available.
      const me = await meAPI.get()
      setMe(me)
      navigate('/')
    } catch {
      setError('Invalid credentials')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="login-bg">
      <form onSubmit={handleSubmit} className="login-form">
        <div className="login-logo">
          otel<span>-magnify</span>
        </div>
        <div className="login-sub">OpAMP Control Plane</div>

        {expired && (
          <div className="login-session-expired" role="status">
            Your session expired. Sign in again to continue.
          </div>
        )}
        {error && <div className="error-text">{error}</div>}

        {hasPassword && (
          <>
            <div className="field">
              <label className="field-label" htmlFor="login-email">
                Email
              </label>
              <input
                id="login-email"
                type="email"
                className="field-input"
                placeholder="ops@example.com"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                autoComplete="email"
              />
            </div>

            <div className="field">
              <label className="field-label" htmlFor="login-password">
                Password
              </label>
              <input
                id="login-password"
                type="password"
                className="field-input"
                placeholder="••••••••"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                autoComplete="current-password"
              />
            </div>

            <button type="submit" className="btn btn-primary login-submit" disabled={loading}>
              {loading ? 'Authenticating...' : 'Sign in'}
            </button>
          </>
        )}

        {ssoMethods.length > 0 && (
          <div className="login-sso">
            {ssoMethods.map((m) => (
              <a key={m.id} href={m.login_url} className="btn btn-secondary login-sso-link">
                Sign in with {m.display_name}
              </a>
            ))}
          </div>
        )}
      </form>
    </div>
  )
}
