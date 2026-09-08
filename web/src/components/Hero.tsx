import { useNavigate } from 'react-router-dom'
import { Info, Play, Star, VolumeX } from 'lucide-react'

import { imageUrl, type HomeRow } from '../lib/api'
import { formatTime } from './ui'

/**
 * Hero is the full-bleed billboard at the top of the home screen.
 *
 * Two gradients do the work: a left-to-right scrim so the text sits on a dark field
 * regardless of the artwork, and a bottom fade that dissolves the image into the page
 * so the first row appears to float over it rather than sitting in a separate box.
 */
export function Hero({ item }: { item: HomeRow & { overview?: string; rating?: number; genres?: string[] } }) {
  const navigate = useNavigate()

  const backdrop = imageUrl(item.backdropId ?? item.posterId, 1280)
  const detailPath = item.kind === 'show' ? `/shows/${item.id}` : `/movies/${item.id}`
  const resuming = (item.resumeSec ?? 0) > 0

  return (
    <section className="relative -mt-16 h-[62vh] min-h-[420px] w-full">
      {backdrop ? (
        <img src={backdrop} alt="" className="absolute inset-0 h-full w-full object-cover object-top" />
      ) : (
        <div className="absolute inset-0 bg-gradient-to-br from-ink-800 to-ink-900" />
      )}

      <div className="scrim-left absolute inset-0" />
      <div className="scrim-bottom absolute inset-x-0 bottom-0 h-2/3" />

      <div className="relative flex h-full flex-col justify-end px-4 pb-24 lg:px-12 lg:pb-32">
        <div className="max-w-xl">
          {item.subtitle && (
            <p className="mb-2 text-sm font-medium uppercase tracking-widest text-accent">{item.subtitle}</p>
          )}

          <h1 className="text-3xl font-black leading-tight text-white drop-shadow-lg sm:text-5xl lg:text-6xl">
            {item.title}
          </h1>

          <div className="mt-3 flex flex-wrap items-center gap-x-3 gap-y-1 text-sm text-ink-200">
            {item.year && <span>{item.year}</span>}
            {item.rating ? (
              <span className="flex items-center gap-1">
                <Star className="h-3.5 w-3.5 fill-accent text-accent" />
                {item.rating.toFixed(1)}
              </span>
            ) : null}
            {item.genres && item.genres.length > 0 && <span>{item.genres.slice(0, 3).join(' · ')}</span>}
            {resuming && item.durationSec ? (
              <span className="text-ink-300">
                {formatTime(item.resumeSec ?? 0)} of {formatTime(item.durationSec)}
              </span>
            ) : null}
          </div>

          {item.overview && (
            <p className="mt-4 line-clamp-3 max-w-lg text-sm leading-relaxed text-ink-200 drop-shadow sm:text-base">
              {item.overview}
            </p>
          )}

          <div className="mt-6 flex flex-wrap items-center gap-3">
            <button className="btn-primary px-8 py-3 text-base" onClick={() => navigate(`/play/${item.id}`)}>
              <Play className="h-5 w-5 fill-current" />
              {resuming ? 'Resume' : 'Play'}
            </button>
            <button
              className="btn-md bg-ink-500/60 px-8 py-3 text-base text-white backdrop-blur hover:bg-ink-500/40"
              onClick={() => navigate(detailPath)}
            >
              <Info className="h-5 w-5" />
              More Info
            </button>
          </div>
        </div>
      </div>

      {/* A decorative nod to the Netflix billboard's mute control. It has nothing to
          mute — the hero is a still — so it is hidden from assistive tech. */}
      <div className="pointer-events-none absolute bottom-32 right-4 hidden lg:block" aria-hidden="true">
        <div className="flex h-10 w-10 items-center justify-center rounded-full border border-ink-400/60 text-ink-300">
          <VolumeX className="h-4 w-4" />
        </div>
      </div>
    </section>
  )
}
