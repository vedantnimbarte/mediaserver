import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from 'react'
import { api, type UserPrefs } from './api'
import { useAuth } from './auth'

const DEFAULTS: UserPrefs = {
  id: '',
  userId: '',
  maxQuality: 'auto',
  autoplayNext: true,
  autoplayCountdown: 10,
  preferredAudioLang: '',
  preferredSubLang: '',
  subtitlesDefaultOn: false,
  seekStepSec: 10,
  skipIntroEnabled: true,
  accentColor: '#E50914',
  posterSize: 'medium',
  showTitles: true,
  reducedMotion: false,
  heroOnHome: true,
}

interface PrefsState {
  prefs: UserPrefs
  loading: boolean
  update: (patch: Partial<UserPrefs>) => Promise<void>
  reset: () => Promise<void>
}

const PrefsContext = createContext<PrefsState | null>(null)

/** Grid column width per density setting, fed to the CSS variable. */
const POSTER_MIN: Record<string, string> = {
  small: '8rem',
  medium: '10rem',
  large: '13rem',
}

/** hexToRgbTriplet converts "#E50914" to "229 9 20" for the CSS custom property. */
function hexToRgbTriplet(hex: string): string | null {
  let value = hex.replace('#', '')
  if (value.length === 3) {
    value = value
      .split('')
      .map((c) => c + c)
      .join('')
  }
  if (value.length !== 6 || !/^[0-9a-f]{6}$/i.test(value)) return null

  const r = parseInt(value.slice(0, 2), 16)
  const g = parseInt(value.slice(2, 4), 16)
  const b = parseInt(value.slice(4, 6), 16)
  return `${r} ${g} ${b}`
}

/** shift lightens or darkens a channel, used to derive the soft/dim accent variants. */
function shift(triplet: string, amount: number): string {
  return triplet
    .split(' ')
    .map((n) => {
      const v = Number(n) + amount
      return String(Math.max(0, Math.min(255, Math.round(v))))
    })
    .join(' ')
}

export function PrefsProvider({ children }: { children: ReactNode }) {
  const { user } = useAuth()
  const [prefs, setPrefs] = useState<UserPrefs>(DEFAULTS)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    if (!user) {
      setPrefs(DEFAULTS)
      setLoading(false)
      return
    }
    let cancelled = false
    api
      .get<UserPrefs>('/preferences')
      .then((p) => {
        if (!cancelled) setPrefs(p)
      })
      .catch(() => {
        // Falling back to defaults keeps the app usable if preferences fail to load.
        if (!cancelled) setPrefs(DEFAULTS)
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [user?.id])

  // Push appearance settings into CSS so every component picks them up without
  // having to thread props through the tree.
  useEffect(() => {
    const root = document.documentElement

    const triplet = hexToRgbTriplet(prefs.accentColor) ?? '229 9 20'
    root.style.setProperty('--accent', triplet)
    root.style.setProperty('--accent-soft', shift(triplet, 28))
    root.style.setProperty('--accent-dim', shift(triplet, -40))
    root.style.setProperty('--poster-min', POSTER_MIN[prefs.posterSize] ?? POSTER_MIN.medium)

    if (prefs.reducedMotion) root.setAttribute('data-reduced-motion', 'true')
    else root.removeAttribute('data-reduced-motion')
  }, [prefs.accentColor, prefs.posterSize, prefs.reducedMotion])

  const update = useCallback(async (patch: Partial<UserPrefs>) => {
    // Apply optimistically: a toggle that visibly lags behind the click feels broken,
    // and these are low-stakes settings.
    setPrefs((current) => ({ ...current, ...patch }))
    try {
      const saved = await api.put<UserPrefs>('/preferences', patch)
      setPrefs(saved)
    } catch (err) {
      // Re-read the server's truth so the UI cannot drift from what was stored.
      try {
        setPrefs(await api.get<UserPrefs>('/preferences'))
      } catch {
        /* keep the optimistic value; the next load will correct it */
      }
      throw err
    }
  }, [])

  const reset = useCallback(async () => {
    setPrefs(await api.post<UserPrefs>('/preferences/reset'))
  }, [])

  return (
    <PrefsContext.Provider value={{ prefs, loading, update, reset }}>{children}</PrefsContext.Provider>
  )
}

export function usePrefs(): PrefsState {
  const ctx = useContext(PrefsContext)
  if (!ctx) throw new Error('usePrefs must be used inside a PrefsProvider')
  return ctx
}
