import { useSearchParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { SearchX } from 'lucide-react'

import { api } from '../lib/api'
import { MediaCard } from '../components/MediaCard'
import { GridSkeleton } from '../components/Skeleton'
import { EmptyState, ErrorBox } from '../components/ui'

interface Hit {
  kind: 'movie' | 'show' | 'episode'
  id: string
  title: string
  subtitle?: string
  posterId?: string
  year?: number
}

export default function SearchResults() {
  const [params] = useSearchParams()
  const query = params.get('q') ?? ''

  const { data, isLoading, error, refetch } = useQuery({
    queryKey: ['search', query],
    queryFn: () => api.get<Hit[]>(`/search?q=${encodeURIComponent(query)}`),
    enabled: query.length > 0,
  })

  if (!query) {
    return (
      <div className="px-4 py-8 lg:px-12">
        <EmptyState
          icon={<SearchX className="h-14 w-14" />}
          title="Search your library"
          message="Type a title in the box above, or press / from anywhere."
        />
      </div>
    )
  }

  if (isLoading) {
    return (
      <div className="px-4 py-8 lg:px-12">
        <div className="skeleton mb-6 h-8 w-64" />
        <GridSkeleton count={12} />
      </div>
    )
  }

  if (error) {
    return (
      <div className="px-4 py-8 lg:px-12">
        <ErrorBox
          title="Search failed"
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

  if (!data || data.length === 0) {
    return (
      <div className="px-4 py-8 lg:px-12">
        <EmptyState
          icon={<SearchX className="h-14 w-14" />}
          title={`No results for “${query}”`}
          message="Try a different spelling, or just part of the title."
        />
      </div>
    )
  }

  return (
    <div className="px-4 py-8 lg:px-12">
      <h1 className="mb-6 text-2xl font-bold text-white">
        Results for “{query}”
        <span className="ml-3 rounded bg-ink-800 px-2 py-0.5 align-middle text-xs font-medium text-ink-300">
          {data.length}
        </span>
      </h1>

      <div className="poster-grid">
        {data.map((hit) => (
          <MediaCard
            key={`${hit.kind}-${hit.id}`}
            item={{
              id: hit.id,
              kind: hit.kind,
              title: hit.title,
              subtitle: hit.subtitle ?? (hit.year ? String(hit.year) : hit.kind),
              posterId: hit.posterId,
              year: hit.year,
              available: true,
            }}
          />
        ))}
      </div>
    </div>
  )
}
