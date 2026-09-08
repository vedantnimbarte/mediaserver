import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from 'react'
import { Modal } from './Modal'

interface Shortcut {
  keys: string[]
  description: string
}

interface ShortcutGroup {
  title: string
  shortcuts: Shortcut[]
}

const GROUPS: ShortcutGroup[] = [
  {
    title: 'Everywhere',
    shortcuts: [
      { keys: ['?'], description: 'Show this help' },
      { keys: ['/'], description: 'Focus search' },
      { keys: ['g', 'h'], description: 'Go home' },
      { keys: ['g', 'm'], description: 'Go to movies' },
      { keys: ['g', 't'], description: 'Go to TV shows' },
      { keys: ['g', 's'], description: 'Go to settings' },
      { keys: ['Esc'], description: 'Close menu or dialog' },
    ],
  },
  {
    title: 'Player',
    shortcuts: [
      { keys: ['Space'], description: 'Play or pause' },
      { keys: ['k'], description: 'Play or pause' },
      { keys: ['←', '→'], description: 'Seek back or forward' },
      { keys: ['j', 'l'], description: 'Seek 30 seconds' },
      { keys: ['↑', '↓'], description: 'Volume up or down' },
      { keys: ['f'], description: 'Fullscreen' },
      { keys: ['m'], description: 'Mute' },
      { keys: ['c'], description: 'Toggle subtitles' },
      { keys: ['n'], description: 'Next episode' },
      { keys: ['0', '–', '9'], description: 'Jump to 0–90% of the runtime' },
    ],
  },
  {
    title: 'Browsing',
    shortcuts: [
      { keys: ['Right click'], description: 'Quick actions on a poster' },
      { keys: ['Enter'], description: 'Open the focused item' },
    ],
  },
]

interface ShortcutsState {
  open: () => void
}

const ShortcutsContext = createContext<ShortcutsState | null>(null)

export function ShortcutsProvider({ children }: { children: ReactNode }) {
  const [isOpen, setIsOpen] = useState(false)

  const open = useCallback(() => setIsOpen(true), [])

  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      // Never hijack a keystroke the user is typing into a field.
      const target = e.target as HTMLElement | null
      if (
        target &&
        (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.isContentEditable)
      ) {
        return
      }
      if (e.key === '?' || (e.key === '/' && e.shiftKey)) {
        e.preventDefault()
        setIsOpen((v) => !v)
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])

  return (
    <ShortcutsContext.Provider value={{ open }}>
      {children}

      <Modal open={isOpen} onClose={() => setIsOpen(false)} title="Keyboard shortcuts" size="lg">
        <div className="grid gap-8 sm:grid-cols-2">
          {GROUPS.map((group) => (
            <div key={group.title}>
              <h3 className="mb-3 text-xs font-semibold uppercase tracking-wider text-ink-400">
                {group.title}
              </h3>
              <dl className="space-y-2">
                {group.shortcuts.map((shortcut) => (
                  <div key={shortcut.description + shortcut.keys.join()} className="flex items-center gap-3">
                    <dt className="flex shrink-0 gap-1">
                      {shortcut.keys.map((key) => (
                        <kbd
                          key={key}
                          className="min-w-[1.75rem] rounded border border-ink-600 bg-ink-800 px-1.5
                                     py-0.5 text-center font-sans text-xs text-ink-200"
                        >
                          {key}
                        </kbd>
                      ))}
                    </dt>
                    <dd className="text-sm text-ink-300">{shortcut.description}</dd>
                  </div>
                ))}
              </dl>
            </div>
          ))}
        </div>
      </Modal>
    </ShortcutsContext.Provider>
  )
}

export function useShortcutsHelp(): ShortcutsState {
  const ctx = useContext(ShortcutsContext)
  if (!ctx) throw new Error('useShortcutsHelp must be used inside a ShortcutsProvider')
  return ctx
}

/**
 * useGlobalNavShortcuts wires the "g then x" navigation chords and "/" for search.
 * The chord state resets after a second so a stray "g" does not linger.
 */
export function useGlobalNavShortcuts(navigate: (path: string) => void, focusSearch: () => void) {
  useEffect(() => {
    let pendingG = false
    let timer: number | null = null

    function reset() {
      pendingG = false
      if (timer) window.clearTimeout(timer)
    }

    function onKey(e: KeyboardEvent) {
      const target = e.target as HTMLElement | null
      if (
        target &&
        (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.isContentEditable)
      ) {
        return
      }
      if (e.metaKey || e.ctrlKey || e.altKey) return

      if (e.key === '/') {
        e.preventDefault()
        focusSearch()
        return
      }

      if (pendingG) {
        const routes: Record<string, string> = {
          h: '/',
          m: '/movies',
          t: '/shows',
          u: '/music',
          p: '/photos',
          s: '/settings',
        }
        const route = routes[e.key.toLowerCase()]
        if (route) {
          e.preventDefault()
          navigate(route)
        }
        reset()
        return
      }

      if (e.key.toLowerCase() === 'g') {
        pendingG = true
        timer = window.setTimeout(reset, 1000)
      }
    }

    window.addEventListener('keydown', onKey)
    return () => {
      window.removeEventListener('keydown', onKey)
      reset()
    }
  }, [navigate, focusSearch])
}
