import { useMemo, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { Tv } from 'lucide-react'

import { api, type Page, type ShowCard } from '../lib/api'
import { usePrefs } from '../lib/prefs'
import { MediaCard } from '../components/MediaCard'
import { GridSkeleton } from '../components/Skeleton'
import { DEFAULT_FILTERS, FilterBar, type Filters } from '../components/FilterBar'
import { EmptyState, ErrorBox } from '../components/ui'

const SORT_OPTIONS = [
  { value: 'title', label: 'A–Z' },
  { value: 'year', label: 'Newest first' },
  { value: 'added', label: 'Recently added' },
]

export default function Shows() {
  const [params] = useSearchParams()
  const libraryId = params.get('libraryId') ?? ''
  const { prefs } = usePrefs()
  const [filters, setFilters] = useState<Filters>(DEFAULT_FILTERS)

  const query = new URLSearchParams({ limit: '500', sort: filters.sort })
  if (libraryId) query.set('libraryId', libraryId)

  const { data, isLoading, error, refetch } = useQuery({
    queryKey: ['shows', libraryId, filters.sort],
    queryFn: () => api.get<Page<ShowCard>>(`/shows?${query.toString()}`),
  })

  const items = data?.items ?? []

  const genres = useMemo(() => Array.from(new Set(items.flatMap((s) => s.genres ?? []))).sort(), [items])
  const years = useMemo(
    () =>
      Array.from(new Set(items.map((s) => s.year).filter((y): y is number => Boolean(y)))).sort(
        (a, b) => b - a,
      ),
    [items],
  )

  const filtered = useMemo(() => {
    return items.filter((s) => {
      if (filters.genre && !(s.genres ?? []).includes(filters.genre)) return false
      if (filters.year && String(s.year ?? '') !== filters.year) return false

      // For a series, "watched" means every available episode has been seen.
      if (filters.watched === 'watched' && s.unwatched > 0) return false
      if (filters.watched === 'unwatched' && s.unwatched === 0) return false
      if (filters.watched === 'inprogress' && !(s.unwatched > 0 && s.unwatched < s.episodeCount)) return false

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
          title="Could not load shows"
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
        title="TV Shows"
        count={filtered.length}
        filters={filters}
        onChange={setFilters}
        genres={genres}
        years={years}
        sortOptions={SORT_OPTIONS}
        showResolution={false}
      />

      {filtered.length === 0 ? (
        <EmptyState
          icon={<Tv className="h-14 w-14" />}
          title={items.length === 0 ? 'No shows found' : 'Nothing matches these filters'}
          message={
            items.length === 0
              ? 'Scan a TV library to fill this page.'
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
          {filtered.map((show) => (
            <MediaCard
              key={show.id}
              item={{
                id: show.id,
                kind: 'show',
                title: show.title,
                subtitle: `${show.seasonCount} season${show.seasonCount === 1 ? '' : 's'}`,
                posterId: show.posterId,
                year: show.year,
                genres: show.genres,
                watched: show.unwatched === 0,
                available: true,
              }}
              onChanged={refetch}
            />
          ))}
        </div>
      )}
    </div>
  )
}
