import { useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Check, ChevronDown, Info, Play, RefreshCw, Tv } from 'lucide-react'

import { api, imageUrl } from '../lib/api'
import { usePrefs } from '../lib/prefs'
import { useToast } from './Toast'
import { useContextMenu, type MenuAction } from './ContextMenu'
import { formatDuration } from './ui'

export interface CardItem {
  id: string
  kind: 'movie' | 'show' | 'episode'
  title: string
  subtitle?: string
  posterId?: string
  backdropId?: string
  year?: number
  progress?: number
  watched?: boolean
  available?: boolean
  durationSec?: number
  genres?: string[]
  overview?: string
  /** For episodes and shows, the parent show to navigate to. */
  showId?: string
}

/** Delay before a hover expands, so sweeping the cursor across a row stays calm. */
const HOVER_DELAY_MS = 450

/**
 * MediaCard is the Netflix-style tile: it lifts and expands on hover to reveal quick
 * actions and a little metadata, and offers the same actions on right-click.
 */
export function MediaCard({
  item,
  wide = false,
  onChanged,
}: {
  item: CardItem
  /** Wide uses a 16:9 backdrop, which suits resume tiles; otherwise a 2:3 poster. */
  wide?: boolean
  onChanged?: () => void
}) {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const toast = useToast()
  const { prefs } = usePrefs()

  const [expanded, setExpanded] = useState(false)
  const timer = useRef<number | null>(null)
  const containerRef = useRef<HTMLDivElement>(null)

  // Reduced motion disables the expansion entirely rather than just shortening it:
  // the point of the setting is that things do not move under the cursor.
  const allowExpand = !prefs.reducedMotion

  const detailPath =
    item.kind === 'movie'
      ? `/movies/${item.id}`
      : item.kind === 'show'
        ? `/shows/${item.id}`
        : `/shows/${item.showId ?? ''}`

  const setWatched = useMutation({
    mutationFn: (watched: boolean) => api.post(`/playstate/${item.id}/watched`, { watched }),
    onSuccess: (_data, watched) => {
      toast.success(watched ? 'Marked as watched' : 'Marked as unwatched', item.title)
      queryClient.invalidateQueries({ queryKey: ['home'] })
      queryClient.invalidateQueries({ queryKey: ['movies'] })
      queryClient.invalidateQueries({ queryKey: ['shows'] })
      onChanged?.()
    },
    onError: (err: any) => toast.error('Could not update', err?.message),
  })

  const refreshMetadata = useMutation({
    mutationFn: () =>
      api.post(item.kind === 'show' ? `/shows/${item.id}/match` : `/movies/${item.id}/match`, {
        tmdbId: 0,
      }),
    onError: () =>
      toast.info(
        'Pick a match first',
        'Open the item and use "Fix match" to choose the right entry.',
      ),
  })

  const actions: MenuAction[] = [
    { label: 'Play', icon: <Play className="h-4 w-4" />, onSelect: () => navigate(`/play/${item.id}`), disabled: item.available === false },
    { label: 'More info', icon: <Info className="h-4 w-4" />, onSelect: () => navigate(detailPath) },
    {
      label: item.watched ? 'Mark as unwatched' : 'Mark as watched',
      icon: <Check className="h-4 w-4" />,
      onSelect: () => setWatched.mutate(!item.watched),
    },
  ]
  if (item.kind === 'episode' && item.showId) {
    actions.push({ label: 'Go to show', icon: <Tv className="h-4 w-4" />, onSelect: () => navigate(`/shows/${item.showId}`) })
  }
  if (item.kind !== 'episode') {
    actions.push({
      label: 'Refresh metadata',
      icon: <RefreshCw className="h-4 w-4" />,
      onSelect: () => refreshMetadata.mutate(),
    })
  }

  const { handlers, menu } = useContextMenu(actions)

  function onEnter() {
    if (!allowExpand) return
    timer.current = window.setTimeout(() => setExpanded(true), HOVER_DELAY_MS)
  }
  function onLeave() {
    if (timer.current) window.clearTimeout(timer.current)
    setExpanded(false)
  }

  const image = wide ? (item.backdropId ?? item.posterId) : item.posterId
  const width = wide ? 500 : 342
  const src = imageUrl(image, width)

  return (
    <>
      <div
        ref={containerRef}
        className={`relative shrink-0 ${wide ? 'w-64 sm:w-72' : 'w-full'}`}
        onMouseEnter={onEnter}
        onMouseLeave={onLeave}
        {...handlers}
      >
        <button
          onClick={() => navigate(item.available === false ? detailPath : `/play/${item.id}`)}
          className={`group relative block w-full overflow-hidden rounded bg-ink-800 text-left
                      transition-transform duration-200 ease-card
                      ${expanded ? 'z-hovercard scale-105 shadow-card' : ''}
                      ${wide ? 'aspect-video' : 'aspect-[2/3]'}`}
          aria-label={item.title}
        >
          {src ? (
            <img src={src} alt="" loading="lazy" className="h-full w-full object-cover" />
          ) : (
            <div className="flex h-full w-full items-center justify-center bg-ink-800 p-2 text-center text-xs text-ink-400">
              {item.title}
            </div>
          )}

          {/* A dark wash on hover so the play affordance reads on bright artwork. */}
          <div
            className={`absolute inset-0 flex items-center justify-center bg-black/40 transition-opacity
                        ${expanded ? 'opacity-100' : 'opacity-0 group-hover:opacity-100'}`}
          >
            <Play className="h-10 w-10 fill-white text-white drop-shadow-lg" />
          </div>

          {item.watched && !item.progress && (
            <span className="absolute left-2 top-2 rounded bg-black/75 px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-ink-200">
              Watched
            </span>
          )}
          {item.available === false && (
            <div className="absolute inset-0 flex items-center justify-center bg-ink-900/80 px-2 text-center text-xs text-ink-300">
              File missing
            </div>
          )}

          {item.progress != null && item.progress > 0 && (
            <div className="absolute inset-x-0 bottom-0 h-1 bg-black/70">
              <div className="h-full bg-accent" style={{ width: `${Math.min(100, item.progress * 100)}%` }} />
            </div>
          )}
        </button>

        {/* The expanded panel: quick actions and a little context, without leaving the row. */}
        {expanded && (
          <div className="absolute inset-x-0 top-full z-hovercard animate-fade-in rounded-b bg-ink-850 p-3 shadow-card">
            <div className="flex items-center gap-2">
              <button
                onClick={() => navigate(`/play/${item.id}`)}
                disabled={item.available === false}
                className="flex h-8 w-8 items-center justify-center rounded-full bg-white text-black transition hover:bg-white/80 disabled:opacity-40"
                aria-label="Play"
              >
                <Play className="h-4 w-4 fill-current" />
              </button>
              <button
                onClick={() => setWatched.mutate(!item.watched)}
                className="btn-icon h-8 w-8 p-0"
                aria-label={item.watched ? 'Mark as unwatched' : 'Mark as watched'}
                title={item.watched ? 'Mark as unwatched' : 'Mark as watched'}
              >
                <Check className={`h-4 w-4 ${item.watched ? 'text-accent' : ''}`} />
              </button>
              <button
                onClick={() => navigate(detailPath)}
                className="btn-icon ml-auto h-8 w-8 p-0"
                aria-label="More info"
                title="More info"
              >
                <ChevronDown className="h-4 w-4" />
              </button>
            </div>

            <p className="mt-2 truncate text-sm font-medium text-white">{item.title}</p>
            <div className="mt-0.5 flex flex-wrap items-center gap-x-2 text-xs text-ink-400">
              {item.subtitle && <span className="truncate">{item.subtitle}</span>}
              {item.year && <span>{item.year}</span>}
              {item.durationSec ? <span>{formatDuration(item.durationSec)}</span> : null}
            </div>
            {item.genres && item.genres.length > 0 && (
              <p className="mt-1 truncate text-xs text-ink-400">{item.genres.slice(0, 3).join(' · ')}</p>
            )}
          </div>
        )}
      </div>

      {/* Static caption under the tile, when the user wants titles shown. */}
      {!wide && prefs.showTitles && !expanded && (
        <div className="mt-2">
          <p className="truncate text-sm text-ink-200">{item.title}</p>
          {(item.subtitle || item.year) && (
            <p className="truncate text-xs text-ink-400">{item.subtitle ?? item.year}</p>
          )}
        </div>
      )}
      {wide && (
        <div className="mt-2 w-64 sm:w-72">
          <p className="truncate text-sm text-ink-200">{item.title}</p>
          {item.subtitle && <p className="truncate text-xs text-ink-400">{item.subtitle}</p>}
        </div>
      )}

      {menu}
    </>
  )
}
