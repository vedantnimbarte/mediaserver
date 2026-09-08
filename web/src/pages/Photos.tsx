import { useCallback, useEffect, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { ChevronLeft, ChevronRight, Image as ImageIcon, X } from 'lucide-react'

import { api, authedUrl, type Photo, type PhotoAlbumCard } from '../lib/api'
import { EmptyState, ErrorBox, Poster, Spinner } from '../components/ui'

interface AlbumDetail {
  id: string
  name: string
  photos: Photo[]
}

export default function Photos() {
  const [params] = useSearchParams()
  const libraryId = params.get('libraryId') ?? ''
  const [albumId, setAlbumId] = useState<string | null>(null)
  const [lightbox, setLightbox] = useState<number | null>(null)

  const query = libraryId ? `?libraryId=${libraryId}` : ''

  const { data: albums, isLoading, error } = useQuery({
    queryKey: ['photoAlbums', libraryId],
    queryFn: () => api.get<PhotoAlbumCard[]>(`/photos/albums${query}`),
  })

  const { data: album } = useQuery({
    queryKey: ['photoAlbum', albumId],
    queryFn: () => api.get<AlbumDetail>(`/photos/albums/${albumId}`),
    enabled: Boolean(albumId),
  })

  const photos = album?.photos ?? []

  const step = useCallback(
    (delta: number) => {
      setLightbox((current) => {
        if (current === null) return null
        const next = current + delta
        if (next < 0 || next >= photos.length) return current
        return next
      })
    },
    [photos.length],
  )

  useEffect(() => {
    if (lightbox === null) return
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') setLightbox(null)
      if (e.key === 'ArrowLeft') step(-1)
      if (e.key === 'ArrowRight') step(1)
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [lightbox, step])

  if (isLoading) return <Spinner label="Loading photos…" />
  if (error) return <ErrorBox title="Could not load photos" message={(error as Error).message} />
  if (!albums || albums.length === 0) {
    return (
      <EmptyState
        icon={<ImageIcon className="h-12 w-12" />}
        title="No photo albums found"
        message="Scan a photo library to fill this page. Each folder becomes an album."
      />
    )
  }

  if (!albumId) {
    return (
      <div>
        <h1 className="mb-6 text-xl font-semibold text-white">
          Photo albums <span className="ml-2 text-sm font-normal text-ink-400">{albums.length}</span>
        </h1>
        <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5">
          {albums.map((a) => (
            <button key={a.id} onClick={() => setAlbumId(a.id)} className="group text-left">
              <div className="aspect-square overflow-hidden rounded-lg bg-ink-800 ring-1 ring-ink-800 transition-all group-hover:ring-2 group-hover:ring-accent">
                {a.coverId ? (
                  <img
                    src={authedUrl(`/api/photos/${a.coverId}/file?w=500`)}
                    alt={a.name}
                    loading="lazy"
                    className="h-full w-full object-cover transition-transform duration-300 group-hover:scale-105"
                  />
                ) : (
                  <Poster alt={a.name} className="h-full w-full" />
                )}
              </div>
              <p className="mt-2 truncate text-sm font-medium text-ink-200">{a.name}</p>
              <p className="text-xs text-ink-400">{a.photoCount} photos</p>
            </button>
          ))}
        </div>
      </div>
    )
  }

  return (
    <div>
      <button className="btn-ghost mb-6" onClick={() => setAlbumId(null)}>
        ← All albums
      </button>
      <h1 className="mb-6 text-xl font-semibold text-white">{album?.name}</h1>

      {!album ? (
        <Spinner />
      ) : (
        // A CSS column layout gives a masonry look without measuring anything in JS.
        <div className="columns-2 gap-3 sm:columns-3 lg:columns-4 xl:columns-5">
          {photos.map((photo, i) => (
            <button
              key={photo.id}
              onClick={() => setLightbox(i)}
              className="mb-3 block w-full break-inside-avoid overflow-hidden rounded-lg bg-ink-800 ring-1 ring-ink-800 transition-all hover:ring-2 hover:ring-accent"
            >
              <img
                src={authedUrl(`/api/photos/${photo.id}/file?w=500`)}
                alt={photo.filename}
                loading="lazy"
                // The known aspect ratio reserves the right space before the image
                // loads, so the grid does not jump around while scrolling.
                style={{ aspectRatio: photo.aspectRatio }}
                className="w-full object-cover"
              />
            </button>
          ))}
        </div>
      )}

      {lightbox !== null && photos[lightbox] && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/95">
          <button
            className="absolute right-4 top-4 rounded-full p-2 text-white/80 hover:bg-white/10"
            onClick={() => setLightbox(null)}
            aria-label="Close"
          >
            <X className="h-6 w-6" />
          </button>

          {lightbox > 0 && (
            <button
              className="absolute left-4 rounded-full p-2 text-white/80 hover:bg-white/10"
              onClick={() => step(-1)}
              aria-label="Previous"
            >
              <ChevronLeft className="h-8 w-8" />
            </button>
          )}
          {lightbox < photos.length - 1 && (
            <button
              className="absolute right-4 rounded-full p-2 text-white/80 hover:bg-white/10"
              onClick={() => step(1)}
              aria-label="Next"
            >
              <ChevronRight className="h-8 w-8" />
            </button>
          )}

          <img
            src={authedUrl(`/api/photos/${photos[lightbox].id}/file?w=1280`)}
            alt={photos[lightbox].filename}
            className="max-h-[88vh] max-w-[92vw] object-contain"
          />

          <div className="absolute inset-x-0 bottom-0 bg-gradient-to-t from-black/80 to-transparent px-6 py-4 text-center text-xs text-white/70">
            <p className="font-medium text-white/90">{photos[lightbox].filename}</p>
            <p className="mt-0.5">
              {[
                photos[lightbox].takenAt ? new Date(photos[lightbox].takenAt!).toLocaleDateString() : null,
                photos[lightbox].camera,
                photos[lightbox].aperture,
                photos[lightbox].shutter,
                photos[lightbox].iso ? `ISO ${photos[lightbox].iso}` : null,
              ]
                .filter(Boolean)
                .join(' · ')}
            </p>
          </div>
        </div>
      )}
    </div>
  )
}
