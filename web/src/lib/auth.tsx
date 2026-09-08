import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from 'react'
import { api, getToken, setToken, type AuthResponse, type ServerInfo, type User } from './api'

interface AuthState {
  user: User | null
  serverInfo: ServerInfo | null
  loading: boolean
  signIn: (username: string, password: string) => Promise<void>
  register: (username: string, password: string) => Promise<void>
  signOut: () => Promise<void>
  refreshServerInfo: () => Promise<void>
}

const AuthContext = createContext<AuthState | null>(null)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(null)
  const [serverInfo, setServerInfo] = useState<ServerInfo | null>(null)
  const [loading, setLoading] = useState(true)

  const refreshServerInfo = useCallback(async () => {
    try {
      setServerInfo(await api.get<ServerInfo>('/server-info'))
    } catch {
      // A server-info failure means the backend is unreachable; the shell renders a
      // connection error rather than an empty screen.
      setServerInfo(null)
    }
  }, [])

  // Restore the session on load: the stored token is only trustworthy if /auth/me
  // still accepts it, since it may have been revoked or expired since last time.
  useEffect(() => {
    let cancelled = false

    async function bootstrap() {
      await refreshServerInfo()
      if (getToken()) {
        try {
          const me = await api.get<User>('/auth/me')
          if (!cancelled) setUser(me)
        } catch {
          setToken(null)
          if (!cancelled) setUser(null)
        }
      }
      if (!cancelled) setLoading(false)
    }

    bootstrap()
    return () => {
      cancelled = true
    }
  }, [refreshServerInfo])

  // The API client dispatches this when any request comes back 401, so a session
  // that expires mid-browse drops straight to the login screen.
  useEffect(() => {
    const onSignedOut = () => setUser(null)
    window.addEventListener('mediaserver:signed-out', onSignedOut)
    return () => window.removeEventListener('mediaserver:signed-out', onSignedOut)
  }, [])

  const applyAuth = useCallback(
    async (resp: AuthResponse) => {
      setToken(resp.token)
      setUser(resp.user)
      await refreshServerInfo()
    },
    [refreshServerInfo],
  )

  const signIn = useCallback(
    async (username: string, password: string) => {
      applyAuth(await api.post<AuthResponse>('/auth/login', { username, password }))
    },
    [applyAuth],
  )

  const register = useCallback(
    async (username: string, password: string) => {
      applyAuth(await api.post<AuthResponse>('/auth/register', { username, password }))
    },
    [applyAuth],
  )

  const signOut = useCallback(async () => {
    try {
      await api.post('/auth/logout')
    } catch {
      // Even if the server call fails, clearing locally is the right outcome.
    }
    setToken(null)
    setUser(null)
    await refreshServerInfo()
  }, [refreshServerInfo])

  return (
    <AuthContext.Provider
      value={{ user, serverInfo, loading, signIn, register, signOut, refreshServerInfo }}
    >
      {children}
    </AuthContext.Provider>
  )
}

export function useAuth(): AuthState {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error('useAuth must be used inside an AuthProvider')
  return ctx
}
