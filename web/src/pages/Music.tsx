import { useEffect, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { Music, Pause, Play, SkipBack, SkipForward, X } from 'lucide-react'

import { api, authedUrl, imageUrl, type Artist, type ArtistCard, type Page, type Track } from '../lib/api'
import { EmptyState, ErrorBox, Poster, Spinner, formatTime } from '../components/ui'

interface QueueItem extends Track {
  albumTitle: string
  artistName: string
  coverId?: string
}

export default function MusicLibrary() {
  const [params] = useSearchParams()
  const libraryId = params.get('libraryId') ?? ''
  const [selectedArtist, setSelectedArtist] = useState<string | null>(null)

  const [queue, setQueue] = useState<QueueItem[]>([])
  const [queueIndex, setQueueIndex] = useState(0)

  const query = new URLSearchParams({ limit: '500' })
  if (libraryId) query.set('libraryId', libraryId)

  const { data, isLoading, error } = useQuery({
    queryKey: ['artists', libraryId],
    queryFn: () => api.get<Page<ArtistCard>>(`/artists?${query.toString()}`),
  })

  const { data: artist } = useQuery({
    queryKey: ['artist', selectedArtist],
    queryFn: () => api.get<Artist>(`/artists/${selectedArtist}`),
    enabled: Boolean(selectedArtist),
  })

  if (isLoading) return <Spinner label="Loading music…" />
  if (error) return <ErrorBox title="Could not load music" message={(error as Error).message} />
  if (!data || data.items.length === 0) {
    return (
      <EmptyState
        icon={<Music className="h-12 w-12" />}
        title="No music found"
        message="Scan a music library to fill this page."
      />
    )
  }

  function playAlbum(tracks: Track[], albumTitle: string, artistName: string, coverId?: string, startAt = 0) {
    setQueue(tracks.map((t) => ({ ...t, albumTitle, artistName, coverId })))
    setQueueIndex(startAt)
  }

  return (
    <div className="pb-24">
      {!selectedArtist ? (
        <>
          <h1 className="mb-6 text-xl font-semibold text-white">
            Artists <span className="ml-2 text-sm font-normal text-ink-400">{data.total}</span>
          </h1>
          <div className="grid grid-cols-3 gap-4 sm:grid-cols-4 md:grid-cols-5 lg:grid-cols-6 xl:grid-cols-8">
            {data.items.map((a) => (
              <button key={a.id} onClick={() => setSelectedArtist(a.id)} className="group text-left">
                <div className="aspect-square overflow-hidden rounded-full bg-ink-800 ring-1 ring-ink-800 transition-all group-hover:ring-2 group-hover:ring-accent">
                  <Poster imageId={a.imageId} alt={a.name} width={342} className="h-full w-full object-cover" />
                </div>
                <p className="mt-2 truncate text-sm font-medium text-ink-200">{a.name}</p>
                <p className="truncate text-xs text-ink-400">
                  {a.albumCount} album{a.albumCount === 1 ? '' : 's'}
                </p>
              </button>
            ))}
          </div>
        </>
      ) : (
        <>
          <button className="btn-ghost mb-6" onClick={() => setSelectedArtist(null)}>
            ← All artists
          </button>

          {!artist ? (
            <Spinner />
          ) : (
            <>
              <h1 className="mb-6 text-2xl font-semibold text-white">{artist.name}</h1>
              <div className="space-y-8">
                {artist.albums.map((album) => (
                  <section key={album.id}>
                    <div className="mb-3 flex items-end gap-4">
                      <div className="h-24 w-24 shrink-0 overflow-hidden rounded-lg bg-ink-800">
                        <Poster imageId={album.coverId} alt={album.title} width={342} className="h-full w-full object-cover" />
                      </div>
                      <div className="min-w-0 flex-1">
                        <h2 className="truncate text-lg font-semibold text-white">{album.title}</h2>
                        <p className="text-sm text-ink-400">
                          {album.year ? `${album.year} · ` : ''}
                          {album.tracks.length} track{album.tracks.length === 1 ? '' : 's'}
                        </p>
                        <button
                          className="btn-primary mt-2"
                          onClick={() => playAlbum(album.tracks, album.title, artist.name, album.coverId)}
                        >
                          <Play className="h-4 w-4 fill-current" />
                          Play album
                        </button>
                      </div>
                    </div>

                    <ol className="divide-y divide-ink-800 rounded-lg border border-ink-800">
                      {album.tracks.map((track, i) => (
                        <li key={track.id}>
                          <button
                            onClick={() => playAlbum(album.tracks, album.title, artist.name, album.coverId, i)}
                            className="flex w-full items-center gap-3 px-4 py-2.5 text-left transition-colors hover:bg-ink-850"
                          >
                            <span className="w-6 shrink-0 text-right text-xs text-ink-500">
                              {track.trackNo || i + 1}
                            </span>
                            <span className="min-w-0 flex-1 truncate text-sm text-ink-200">{track.title}</span>
                            <span className="shrink-0 text-xs tabular-nums text-ink-500">
                              {formatTime(track.durationSec ?? 0)}
                            </span>
                          </button>
                        </li>
                      ))}
                    </ol>
                  </section>
                ))}
              </div>
            </>
          )}
        </>
      )}

      {queue.length > 0 && (
        <NowPlaying
          queue={queue}
          index={queueIndex}
          onIndexChange={setQueueIndex}
          onClose={() => setQueue([])}
        />
      )}
    </div>
  )
}

/** NowPlaying is the persistent audio bar; it owns the single <audio> element. */
function NowPlaying({
  queue,
  index,
  onIndexChange,
  onClose,
}: {
  queue: QueueItem[]
  index: number
  onIndexChange: (i: number) => void
  onClose: () => void
}) {
  const audioRef = useRef<HTMLAudioElement>(null)
  const [playing, setPlaying] = useState(true)
  const [position, setPosition] = useState(0)

  const track = queue[index]

  // Loading a new source and playing it must happen together, or the browser
  // silently keeps playing the previous track.
  useEffect(() => {
    const audio = audioRef.current
    if (!audio || !track) return
    audio.src = authedUrl(`/api/media/${track.id}/file`)
    audio.play().then(() => setPlaying(true)).catch(() => setPlaying(false))
  }, [track?.id])

  if (!track) return null

  function next() {
    if (index + 1 < queue.length) onIndexChange(index + 1)
    else onClose()
  }

  return (
    <div className="fixed inset-x-0 bottom-0 z-40 border-t border-ink-800 bg-ink-900/95 px-4 py-3 backdrop-blur lg:pl-64">
      <audio
        ref={audioRef}
        onPlay={() => setPlaying(true)}
        onPause={() => setPlaying(false)}
        onTimeUpdate={(e) => setPosition(e.currentTarget.currentTime)}
        onEnded={next}
      />

      <div className="flex items-center gap-4">
        <div className="h-12 w-12 shrink-0 overflow-hidden rounded bg-ink-800">
          <img src={imageUrl(track.coverId, 154) ?? ''} alt="" className="h-full w-full object-cover" />
        </div>

        <div className="min-w-0 flex-1">
          <p className="truncate text-sm font-medium text-ink-100">{track.title}</p>
          <p className="truncate text-xs text-ink-400">
            {track.artistName} · {track.albumTitle}
          </p>
        </div>

        <span className="hidden text-xs tabular-nums text-ink-500 sm:block">
          {formatTime(position)} / {formatTime(track.durationSec ?? 0)}
        </span>

        <div className="flex items-center gap-1">
          <button
            className="rounded p-2 text-ink-300 hover:bg-ink-800 disabled:opacity-40"
            onClick={() => onIndexChange(index - 1)}
            disabled={index === 0}
            aria-label="Previous track"
          >
            <SkipBack className="h-5 w-5" />
          </button>
          <button
            className="rounded-full bg-accent p-2 text-ink-950 hover:bg-accent-soft"
            onClick={() => {
              const audio = audioRef.current
              if (!audio) return
              if (audio.paused) audio.play().catch(() => {})
              else audio.pause()
            }}
            aria-label={playing ? 'Pause' : 'Play'}
          >
            {playing ? <Pause className="h-5 w-5" /> : <Play className="h-5 w-5 fill-current" />}
          </button>
          <button
            className="rounded p-2 text-ink-300 hover:bg-ink-800 disabled:opacity-40"
            onClick={next}
            disabled={index + 1 >= queue.length}
            aria-label="Next track"
          >
            <SkipForward className="h-5 w-5" />
          </button>
          <button className="rounded p-2 text-ink-400 hover:bg-ink-800" onClick={onClose} aria-label="Close player">
            <X className="h-5 w-5" />
          </button>
        </div>
      </div>
    </div>
  )
}
