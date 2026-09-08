import { useState } from 'react'

import { useAuth } from '../lib/auth'

export default function Login() {
  const { serverInfo, signIn, register } = useAuth()
  const isSetup = serverInfo?.setupRequired ?? false

  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [confirm, setConfirm] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault()
    setError(null)

    if (isSetup && password !== confirm) {
      setError('The passwords do not match.')
      return
    }

    setBusy(true)
    try {
      if (isSetup) await register(username, password)
      else await signIn(username, password)
    } catch (err: any) {
      setError(err?.message ?? 'Something went wrong.')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="relative flex h-full items-center justify-center overflow-hidden px-4">
      {/* A soft accent glow behind the form, so the sign-in screen is not a flat
          black rectangle. */}
      <div
        aria-hidden="true"
        className="pointer-events-none absolute left-1/2 top-1/3 h-[38rem] w-[38rem] -translate-x-1/2
                   -translate-y-1/2 rounded-full opacity-20 blur-3xl"
        style={{ background: 'radial-gradient(circle, rgb(var(--accent)) 0%, transparent 70%)' }}
      />

      <div className="relative w-full max-w-sm">
        <div className="mb-10 text-center">
          <span className="wordmark text-6xl">KINO</span>
          <p className="mt-4 text-sm text-ink-300">
            {isSetup ? 'Create the administrator account to get started.' : 'Sign in to continue.'}
          </p>
        </div>

        <form onSubmit={onSubmit} className="space-y-4 rounded-lg border border-ink-700/60 bg-ink-850/80 p-6 backdrop-blur">
          <div>
            <label className="label" htmlFor="username">
              Username
            </label>
            <input
              id="username"
              className="input"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              autoComplete="username"
              autoFocus
              required
            />
          </div>

          <div>
            <label className="label" htmlFor="password">
              Password
            </label>
            <input
              id="password"
              type="password"
              className="input"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              autoComplete={isSetup ? 'new-password' : 'current-password'}
              required
            />
            {isSetup && <p className="hint">At least 8 characters.</p>}
          </div>

          {isSetup && (
            <div>
              <label className="label" htmlFor="confirm">
                Confirm password
              </label>
              <input
                id="confirm"
                type="password"
                className="input"
                value={confirm}
                onChange={(e) => setConfirm(e.target.value)}
                autoComplete="new-password"
                required
              />
            </div>
          )}

          {error && (
            <p
              role="alert"
              className="rounded border border-red-900/60 bg-red-950/40 px-3 py-2 text-sm text-red-200"
            >
              {error}
            </p>
          )}

          <button type="submit" className="btn-accent w-full py-3" disabled={busy}>
            {busy ? 'Please wait…' : isSetup ? 'Create account' : 'Sign in'}
          </button>
        </form>

        {!serverInfo?.ffmpegReady && (
          <p className="mt-4 rounded border border-amber-900/50 bg-amber-950/30 px-3 py-2 text-xs leading-relaxed text-amber-200">
            ffmpeg was not found, so scanning and playback will not work yet.
            {serverInfo?.ffmpegHint ? ` ${serverInfo.ffmpegHint}` : ''}
          </p>
        )}
      </div>
    </div>
  )
}
