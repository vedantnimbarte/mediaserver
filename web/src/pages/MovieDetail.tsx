import { useNavigate, useParams } from 'react-router-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, Play, RotateCcw, Star } from 'lucide-react'

import { api, imageUrl, type MovieDetail as Movie } from '../lib/api'
import { ErrorBox, Poster, Spinner, formatBytes, formatDuration, formatTime } from '../components/ui'

export default function MovieDetail() {
  const { id = '' } = useParams()
  const navigate = useNavigate()
  const queryClient = useQueryClient()

  const { data: movie, isLoading, error } = useQuery({
    queryKey: ['movie', id],
    queryFn: () => api.get<Movie>(`/movies/${id}`),
  })

  const setWatched = useMutation({
    mutationFn: (watched: boolean) => api.post(`/playstate/${id}/watched`, { watched }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['movie', id] })
      queryClient.invalidateQueries({ queryKey: ['home'] })
      queryClient.invalidateQueries({ queryKey: ['movies'] })
    },
  })

  if (isLoading) return <Spinner />
  if (error || !movie) return <ErrorBox title="Could not load this movie" message={(error as Error)?.message} />

  const backdrop = imageUrl(movie.backdropId, 1280)
  const video = movie.media.streams?.find((s) => s.kind === 'video')
  const resumable = movie.resumeSec > 0

  return (
    <div className="-mx-4 -mt-6 lg:-mx-8">
      {backdrop && (
        <div className="relative h-56 overflow-hidden sm:h-72 lg:h-96">
          <img src={backdrop} alt="" className="h-full w-full object-cover" />
          {/* The gradient keeps text legible over any artwork. */}
          <div className="absolute inset-0 bg-gradient-to-t from-ink-950 via-ink-950/70 to-ink-950/20" />
        </div>
      )}

      <div className={`px-4 lg:px-8 ${backdrop ? '-mt-28 relative' : 'pt-6'}`}>
        <div className="flex flex-col gap-6 sm:flex-row">
          <div className="w-40 shrink-0 sm:w-52">
            <div className="aspect-[2/3] overflow-hidden rounded-xl shadow-2xl ring-1 ring-ink-800">
              <Poster imageId={movie.posterId} alt={movie.title} width={500} className="h-full w-full object-cover" />
            </div>
          </div>

          <div className="min-w-0 flex-1">
            <h1 className="text-2xl font-bold text-white sm:text-3xl">{movie.title}</h1>

            <div className="mt-2 flex flex-wrap items-center gap-x-3 gap-y-1 text-sm text-ink-400">
              {movie.year && <span>{movie.year}</span>}
              {movie.runtimeMins ? <span>{formatDuration(movie.runtimeMins * 60)}</span> : null}
              {!movie.runtimeMins && movie.media.durationSec ? (
                <span>{formatDuration(movie.media.durationSec)}</span>
              ) : null}
              {movie.rating ? (
                <span className="flex items-center gap-1">
                  <Star className="h-3.5 w-3.5 fill-accent text-accent" />
                  {movie.rating.toFixed(1)}
                </span>
              ) : null}
              {video && <span>{video.height}p</span>}
              {movie.media.container && <span className="uppercase">{movie.media.container.split(',')[0]}</span>}
            </div>

            {movie.tagline && <p className="mt-3 text-sm italic text-ink-400">{movie.tagline}</p>}

            {movie.genres && movie.genres.length > 0 && (
              <div className="mt-3 flex flex-wrap gap-2">
                {movie.genres.map((g) => (
                  <span key={g} className="rounded-full bg-ink-800 px-2.5 py-1 text-xs text-ink-300">
                    {g}
                  </span>
                ))}
              </div>
            )}

            <div className="mt-5 flex flex-wrap gap-3">
              {!movie.media.available ? (
                <p className="rounded-lg border border-amber-900/60 bg-amber-950/30 px-3 py-2 text-sm text-amber-200">
                  The file for this movie is missing from disk. Rescan the library to update it.
                </p>
              ) : (
                <>
                  <button className="btn-primary" onClick={() => navigate(`/play/${movie.id}`)}>
                    <Play className="h-4 w-4 fill-current" />
                    {resumable ? `Resume at ${formatTime(movie.resumeSec)}` : 'Play'}
                  </button>
                  {resumable && (
                    <button className="btn-ghost" onClick={() => navigate(`/play/${movie.id}?from=0`)}>
                      <RotateCcw className="h-4 w-4" />
                      Start over
                    </button>
                  )}
                  <button
                    className="btn-ghost"
                    onClick={() => setWatched.mutate(!movie.watched)}
                    disabled={setWatched.isPending}
                  >
                    <Check className="h-4 w-4" />
                    {movie.watched ? 'Mark unwatched' : 'Mark watched'}
                  </button>
                </>
              )}
            </div>

            {movie.overview && <p className="mt-6 max-w-3xl text-sm leading-relaxed text-ink-300">{movie.overview}</p>}

            <dl className="mt-6 grid max-w-3xl grid-cols-1 gap-x-8 gap-y-2 text-sm sm:grid-cols-2">
              {movie.directors && movie.directors.length > 0 && (
                <Detail label="Director" value={movie.directors.join(', ')} />
              )}
              {movie.studios && movie.studios.length > 0 && (
                <Detail label="Studio" value={movie.studios.slice(0, 2).join(', ')} />
              )}
              {movie.library && <Detail label="Library" value={movie.library} />}
              <Detail label="File size" value={formatBytes(movie.media.size)} />
            </dl>

            {movie.metaStatus === 'nomatch' && (
              <p className="mt-4 max-w-3xl rounded-lg border border-ink-700 bg-ink-900 px-3 py-2 text-xs text-ink-400">
                No metadata match was found for this file, so the details above come from its filename.
              </p>
            )}
          </div>
        </div>

        {movie.cast && movie.cast.length > 0 && (
          <section className="mt-10">
            <h2 className="mb-3 text-lg font-semibold text-white">Cast</h2>
            <div className="shelf">
              {movie.cast.map((member) => (
                <div key={`${member.name}-${member.order}`} className="w-24 shrink-0 text-center">
                  <div className="aspect-square overflow-hidden rounded-full bg-ink-800">
                    <Poster imageId={member.profileId} alt={member.name} width={185} className="h-full w-full object-cover" />
                  </div>
                  <p className="mt-2 truncate text-xs font-medium text-ink-200">{member.name}</p>
                  {member.character && <p className="truncate text-xs text-ink-500">{member.character}</p>}
                </div>
              ))}
            </div>
          </section>
        )}

        <div className="h-10" />
      </div>
    </div>
  )
}

function Detail({ label, value }: { label: string; value: string }) {
  if (!value) return null
  return (
    <div className="flex gap-2">
      <dt className="shrink-0 text-ink-500">{label}</dt>
      <dd className="truncate text-ink-300">{value}</dd>
    </div>
  )
}
