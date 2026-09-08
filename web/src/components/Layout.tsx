import { useCallback, useEffect, useRef, useState } from 'react'
import { Link, NavLink, useNavigate } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import {
  Clapperboard,
  Image as ImageIcon,
  Keyboard,
  LogOut,
  Menu,
  Music,
  Search,
  Settings as SettingsIcon,
  Tv,
  X,
} from 'lucide-react'

import { api, type Library } from '../lib/api'
import { useAuth } from '../lib/auth'
import { useGlobalNavShortcuts, useShortcutsHelp } from './Shortcuts'

const typeIcon: Record<string, React.ReactNode> = {
  movie: <Clapperboard className="h-4 w-4" />,
  show: <Tv className="h-4 w-4" />,
  music: <Music className="h-4 w-4" />,
  photo: <ImageIcon className="h-4 w-4" />,
}

const typeRoute: Record<string, string> = {
  movie: '/movies',
  show: '/shows',
  music: '/music',
  photo: '/photos',
}

export default function Layout({ children }: { children: React.ReactNode }) {
  const { user, signOut } = useAuth()
  const navigate = useNavigate()
  const shortcuts = useShortcutsHelp()

  const [menuOpen, setMenuOpen] = useState(false)
  const [query, setQuery] = useState('')
  const [scrolled, setScrolled] = useState(false)
  const searchRef = useRef<HTMLInputElement>(null)

  const { data: libraries } = useQuery({
    queryKey: ['libraries'],
    queryFn: () => api.get<Library[]>('/libraries'),
  })

  const focusSearch = useCallback(() => searchRef.current?.focus(), [])
  useGlobalNavShortcuts(navigate, focusSearch)

  // The header starts transparent so the hero runs behind it, then gains a solid
  // background once the page scrolls — the same trick Netflix uses.
  const mainRef = useRef<HTMLElement>(null)
  useEffect(() => {
    const el = mainRef.current
    if (!el) return
    const onScroll = () => setScrolled(el.scrollTop > 40)
    el.addEventListener('scroll', onScroll, { passive: true })
    return () => el.removeEventListener('scroll', onScroll)
  }, [])

  function onSearch(e: React.FormEvent) {
    e.preventDefault()
    const term = query.trim()
    if (term) navigate(`/search?q=${encodeURIComponent(term)}`)
  }

  const navLinkClass = ({ isActive }: { isActive: boolean }) =>
    `flex items-center gap-3 rounded px-3 py-2 text-sm transition-colors ${
      isActive ? 'bg-ink-700 text-white' : 'text-ink-300 hover:bg-ink-800 hover:text-white'
    }`

  const sidebar = (
    <nav className="flex h-full flex-col gap-1 p-3">
      <Link
        to="/"
        className="mb-6 block px-2 pt-2"
        onClick={() => setMenuOpen(false)}
        aria-label="KINO home"
      >
        <span className="wordmark text-3xl">KINO</span>
      </Link>

      <NavLink to="/" end className={navLinkClass} onClick={() => setMenuOpen(false)}>
        <Clapperboard className="h-4 w-4" />
        Home
      </NavLink>

      {libraries && libraries.length > 0 && (
        <p className="mt-5 px-3 pb-1 text-[11px] font-semibold uppercase tracking-wider text-ink-500">
          Libraries
        </p>
      )}
      {libraries?.map((lib) => (
        <NavLink
          key={lib.id}
          to={`${typeRoute[lib.type] ?? '/'}?libraryId=${lib.id}`}
          className={navLinkClass}
          onClick={() => setMenuOpen(false)}
        >
          {typeIcon[lib.type]}
          <span className="flex-1 truncate">{lib.name}</span>
          {lib.scanning ? (
            <span className="text-[11px] font-medium text-accent">scanning</span>
          ) : (
            <span className="text-[11px] tabular-nums text-ink-500">{lib.itemCount}</span>
          )}
        </NavLink>
      ))}

      <div className="mt-auto space-y-1 border-t border-ink-700/60 pt-3">
        <button
          onClick={() => {
            setMenuOpen(false)
            shortcuts.open()
          }}
          className="flex w-full items-center gap-3 rounded px-3 py-2 text-sm text-ink-300 transition-colors hover:bg-ink-800 hover:text-white"
        >
          <Keyboard className="h-4 w-4" />
          Shortcuts
          <kbd className="ml-auto rounded border border-ink-600 px-1.5 text-[11px] text-ink-400">?</kbd>
        </button>

        {user?.isAdmin && (
          <NavLink to="/settings" className={navLinkClass} onClick={() => setMenuOpen(false)}>
            <SettingsIcon className="h-4 w-4" />
            Settings
          </NavLink>
        )}

        <button
          onClick={() => {
            setMenuOpen(false)
            signOut()
          }}
          className="flex w-full items-center gap-3 rounded px-3 py-2 text-sm text-ink-300 transition-colors hover:bg-ink-800 hover:text-white"
        >
          <LogOut className="h-4 w-4" />
          Sign out
          <span className="ml-auto max-w-20 truncate text-[11px] text-ink-500">{user?.username}</span>
        </button>
      </div>
    </nav>
  )

  return (
    <div className="flex h-full">
      <aside className="hidden w-60 shrink-0 border-r border-ink-800 bg-ink-950 lg:block">{sidebar}</aside>

      {menuOpen && (
        <div className="fixed inset-0 z-bar lg:hidden">
          <div className="absolute inset-0 bg-black/70" onClick={() => setMenuOpen(false)} />
          <aside className="absolute inset-y-0 left-0 w-64 animate-slide-up border-r border-ink-800 bg-ink-950">
            {sidebar}
          </aside>
        </div>
      )}

      <div className="relative flex min-w-0 flex-1 flex-col">
        <header
          className={`absolute inset-x-0 top-0 z-bar flex items-center gap-3 px-4 py-3 transition-colors duration-300 lg:px-8 ${
            scrolled ? 'bg-ink-900/95 backdrop-blur' : 'bg-gradient-to-b from-ink-900/90 to-transparent'
          }`}
        >
          <button
            className="rounded p-2 text-ink-200 hover:bg-white/10 lg:hidden"
            onClick={() => setMenuOpen((v) => !v)}
            aria-label="Toggle navigation"
          >
            {menuOpen ? <X className="h-5 w-5" /> : <Menu className="h-5 w-5" />}
          </button>

          <span className="wordmark text-xl lg:hidden">KINO</span>

          <form onSubmit={onSearch} className="relative ml-auto w-full max-w-xs">
            <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-ink-400" />
            <input
              ref={searchRef}
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="Search"
              className="input border-ink-600/70 bg-black/60 py-2 pl-9 backdrop-blur"
              aria-label="Search"
            />
          </form>
        </header>

        <main ref={mainRef} className="flex-1 overflow-y-auto pt-16">
          {children}
        </main>
      </div>
    </div>
  )
}
