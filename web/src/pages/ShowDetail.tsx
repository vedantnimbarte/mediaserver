import { useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, Play, Star } from 'lucide-react'

import { api, imageUrl, type Episode, type ShowDetail as Show } from '../lib/api'
import { ErrorBox, Poster, ProgressBar, Spinner, formatDuration, formatTime } from '../components/ui'

export default function ShowDetail() {
  const { id = '' } = useParams()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [seasonNumber, setSeasonNumber] = useState<number | null>(null)

  const { data: show, isLoading, error } = useQuery({
    queryKey: ['show', id],
    queryFn: () => api.get<Show>(`/shows/${id}`),
  })

  const markSeason = useMutation({
    mutationFn: ({ season, watched }: { season: number; watched: boolean }) =>
      api.post(`/shows/${id}/watched?season=${season}`, { watched }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['show', id] })
      queryClient.invalidateQueries({ queryKey: ['home'] })
      queryClient.invalidateQueries({ queryKey: ['shows'] })
    },
  })

  if (isLoading) return <Spinner />
  if (error || !show) return <ErrorBox title="Could not load this show" message={(error as Error)?.message} />

  const activeSeason =
    show.seasons.find((s) => s.number === seasonNumber) ?? show.seasons[0]
  const backdrop = imageUrl(show.backdropId, 1280)

  return (
    <div className="-mx-4 -mt-6 lg:-mx-8">
      {backdrop && (
        <div className="relative h-56 overflow-hidden sm:h-72 lg:h-80">
          <img src={backdrop} alt="" className="h-full w-full object-cover" />
          <div className="absolute inset-0 bg-gradient-to-t from-ink-950 via-ink-950/70 to-ink-950/20" />
        </div>
      )}

      <div className={`px-4 lg:px-8 ${backdrop ? '-mt-24 relative' : 'pt-6'}`}>
        <div className="flex flex-col gap-6 sm:flex-row">
          <div className="w-36 shrink-0 sm:w-48">
            <div className="aspect-[2/3] overflow-hidden rounded-xl shadow-2xl ring-1 ring-ink-800">
              <Poster imageId={show.posterId} alt={show.title} width={500} className="h-full w-full object-cover" />
            </div>
          </div>

          <div className="min-w-0 flex-1">
            <h1 className="text-2xl font-bold text-white sm:text-3xl">{show.title}</h1>

            <div className="mt-2 flex flex-wrap items-center gap-x-3 gap-y-1 text-sm text-ink-400">
              {show.year && <span>{show.year}</span>}
              <span>
                {show.seasons.length} season{show.seasons.length === 1 ? '' : 's'}
              </span>
              {show.status && <span>{show.status}</span>}
              {show.rating ? (
                <span className="flex items-center gap-1">
                  <Star className="h-3.5 w-3.5 fill-accent text-accent" />
                  {show.rating.toFixed(1)}
                </span>
              ) : null}
            </div>

            {show.genres && show.genres.length > 0 && (
              <div className="mt-3 flex flex-wrap gap-2">
                {show.genres.map((g) => (
                  <span key={g} className="rounded-full bg-ink-800 px-2.5 py-1 text-xs text-ink-300">
                    {g}
                  </span>
                ))}
              </div>
            )}

            {/* "Next up" is the single most useful button on this page: it is what the
                viewer almost always wants, without hunting through the season list. */}
            {show.nextUp && (
              <div className="mt-5">
                <button className="btn-primary" onClick={() => navigate(`/play/${show.nextUp!.id}`)}>
                  <Play className="h-4 w-4 fill-current" />
                  {show.nextUp.resumeSec > 0 ? 'Resume' : 'Play'} S
                  {String(show.nextUp.season).padStart(2, '0')}E
                  {String(show.nextUp.episode).padStart(2, '0')}
                  {show.nextUp.title ? ` · ${show.nextUp.title}` : ''}
                </button>
              </div>
            )}

            {show.overview && <p className="mt-5 max-w-3xl text-sm leading-relaxed text-ink-300">{show.overview}</p>}
          </div>
        </div>

        <section className="mt-10">
          <div className="mb-4 flex flex-wrap items-center gap-3">
            <h2 className="mr-auto text-lg font-semibold text-white">Episodes</h2>

            {show.seasons.length > 1 && (
              <select
                className="input w-auto"
                value={activeSeason?.number ?? 0}
                onChange={(e) => setSeasonNumber(Number(e.target.value))}
                aria-label="Season"
              >
                {show.seasons.map((s) => (
                  <option key={s.number} value={s.number}>
                    {s.number === 0 ? 'Specials' : `Season ${s.number}`}
                  </option>
                ))}
              </select>
            )}

            {activeSeason && (
              <button
                className="btn-ghost"
                disabled={markSeason.isPending}
                onClick={() =>
                  markSeason.mutate({
                    season: activeSeason.number,
                    watched: !activeSeason.episodes.every((e) => e.watched),
                  })
                }
              >
                <Check className="h-4 w-4" />
                {activeSeason.episodes.every((e) => e.watched) ? 'Mark season unwatched' : 'Mark season watched'}
              </button>
            )}
          </div>

          <div className="space-y-2">
            {activeSeason?.episodes.map((ep) => (
              <EpisodeRow key={ep.id} episode={ep} onPlay={() => navigate(`/play/${ep.id}`)} />
            ))}
          </div>
        </section>

        <div className="h-10" />
      </div>
    </div>
  )
}

function EpisodeRow({ episode, onPlay }: { episode: Episode; onPlay: () => void }) {
  const missing = !episode.media.available

  return (
    <div
      className={`group flex gap-4 rounded-lg border border-ink-800 bg-ink-900/40 p-3 transition-colors ${
        missing ? 'opacity-50' : 'hover:border-ink-700 hover:bg-ink-850'
      }`}
    >
      <button
        onClick={onPlay}
        disabled={missing}
        className="relative aspect-video w-32 shrink-0 overflow-hidden rounded bg-ink-800 sm:w-40 disabled:cursor-not-allowed"
        aria-label={`Play episode ${episode.episode}`}
      >
        <Poster imageId={episode.stillId} alt="" width={342} className="h-full w-full object-cover" />
        {!missing && (
          <span className="absolute inset-0 flex items-center justify-center bg-black/40 opacity-0 transition-opacity group-hover:opacity-100">
            <Play className="h-8 w-8 fill-white text-white" />
          </span>
        )}
        <ProgressBar value={episode.progress} />
      </button>

      <div className="min-w-0 flex-1">
        <div className="flex items-baseline gap-2">
          <span className="shrink-0 text-sm font-semibold text-ink-400">{episode.episode}.</span>
          <h3 className="truncate text-sm font-medium text-ink-100">
            {episode.title || `Episode ${episode.episode}`}
          </h3>
          {episode.watched && <Check className="h-4 w-4 shrink-0 text-accent" />}
        </div>

        <div className="mt-1 flex flex-wrap gap-x-3 text-xs text-ink-500">
          {episode.airDate && <span>{episode.airDate}</span>}
          {episode.durationSec ? <span>{formatDuration(episode.durationSec)}</span> : null}
          {episode.resumeSec > 0 && <span className="text-accent">Resume at {formatTime(episode.resumeSec)}</span>}
          {missing && <span className="text-amber-400">File missing</span>}
        </div>

        {episode.overview && (
          <p className="mt-1.5 line-clamp-2 text-xs leading-relaxed text-ink-400">{episode.overview}</p>
        )}
      </div>
    </div>
  )
}
