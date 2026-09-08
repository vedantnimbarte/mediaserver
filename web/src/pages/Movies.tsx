import { useMemo, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'

import { api, type MovieCard as MovieCardType, type Page } from '../lib/api'
import { usePrefs } from '../lib/prefs'
import { MediaCard } from '../components/MediaCard'
import { GridSkeleton } from '../components/Skeleton'
import { DEFAULT_FILTERS, FilterBar, resolutionOf, type Filters } from '../components/FilterBar'
import { EmptyState, ErrorBox } from '../components/ui'

const SORT_OPTIONS = [
  { value: 'title', label: 'A–Z' },
  { value: 'year', label: 'Newest first' },
  { value: 'added', label: 'Recently added' },
  { value: 'rating', label: 'Highest rated' },
  { value: 'runtime', label: 'Longest' },
]

export default function Movies() {
  const [params] = useSearchParams()
  const libraryId = params.get('libraryId') ?? ''
  const { prefs } = usePrefs()
  const [filters, setFilters] = useState<Filters>(DEFAULT_FILTERS)

  // Only the sort and library go to the server; the rest are applied client-side so
  // toggling a chip is instant and the genre and year lists can be derived from the
  // data that is already loaded.
  const query = new URLSearchParams({ limit: '500', sort: filters.sort })
  if (libraryId) query.set('libraryId', libraryId)

  const { data, isLoading, error, refetch } = useQuery({
    queryKey: ['movies', libraryId, filters.sort],
    queryFn: () => api.get<Page<MovieCardType>>(`/movies?${query.toString()}`),
  })

  const items = data?.items ?? []

  const genres = useMemo(
    () => Array.from(new Set(items.flatMap((m) => m.genres ?? []))).sort(),
    [items],
  )
  const years = useMemo(
    () =>
      Array.from(new Set(items.map((m) => m.year).filter((y): y is number => Boolean(y)))).sort(
        (a, b) => b - a,
      ),
    [items],
  )

  const filtered = useMemo(() => {
    return items.filter((m) => {
      if (filters.genre && !(m.genres ?? []).includes(filters.genre)) return false
      if (filters.year && String(m.year ?? '') !== filters.year) return false

      if (filters.watched === 'watched' && !m.watched) return false
      if (filters.watched === 'unwatched' && (m.watched || (m.progress ?? 0) > 0)) return false
      if (filters.watched === 'inprogress' && !((m.progress ?? 0) > 0 && !m.watched)) return false

      if (filters.resolution !== 'all' && resolutionOf(m.height) !== filters.resolution) return false

      return true
    })
  }, [items, filters])

  if (isLoading) {
    return (
      <div className="px-4 py-8 lg:px-12">
        <div className="skeleton mb-6 h-8 w-48" />
        <GridSkeleton showTitle={prefs.showTitles} />
      </div>
    )
  }

  if (error) {
    return (
      <div className="px-4 py-8 lg:px-12">
        <ErrorBox
          title="Could not load movies"
          message={(error as Error).message}
          action={
            <button className="btn-primary" onClick={() => refetch()}>
              Try again
            </button>
          }
        />
      </div>
    )
  }

  return (
    <div className="px-4 py-8 lg:px-12">
      <FilterBar
        title="Movies"
        count={filtered.length}
        filters={filters}
        onChange={setFilters}
        genres={genres}
        years={years}
        sortOptions={SORT_OPTIONS}
      />

      {filtered.length === 0 ? (
        <EmptyState
          title={items.length === 0 ? 'No movies found' : 'Nothing matches these filters'}
          message={
            items.length === 0
              ? 'Scan a movie library to fill this page.'
              : 'Try loosening or clearing the filters above.'
          }
          action={
            items.length > 0 ? (
              <button className="btn-primary" onClick={() => setFilters({ ...DEFAULT_FILTERS, sort: filters.sort })}>
                Clear filters
              </button>
            ) : undefined
          }
        />
      ) : (
        <div className="poster-grid">
          {filtered.map((movie) => (
            <MediaCard
              key={movie.id}
              item={{
                id: movie.id,
                kind: 'movie',
                title: movie.title,
                year: movie.year,
                posterId: movie.posterId,
                progress: movie.progress,
                watched: movie.watched,
                available: movie.available,
                genres: movie.genres,
                durationSec: movie.durationSec,
              }}
              onChanged={refetch}
            />
          ))}
        </div>
      )}
    </div>
  )
}
