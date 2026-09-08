import { useMemo } from 'react'
import { Link } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { Settings as SettingsIcon } from 'lucide-react'

import { api, type HomeData, type HomeRow, type Library, type MovieDetail } from '../lib/api'
import { usePrefs } from '../lib/prefs'
import { useAuth } from '../lib/auth'
import { Hero } from '../components/Hero'
import { Row } from '../components/Row'
import { MediaCard, type CardItem } from '../components/MediaCard'
import { HomeSkeleton } from '../components/Skeleton'
import { EmptyState, ErrorBox } from '../components/ui'

export default function Home() {
  const { user } = useAuth()
  const { prefs } = usePrefs()

  const { data: libraries } = useQuery({
    queryKey: ['libraries'],
    queryFn: () => api.get<Library[]>('/libraries'),
  })

  const { data, isLoading, error, refetch } = useQuery({
    queryKey: ['home'],
    queryFn: () => api.get<HomeData>('/home'),
  })

  // The billboard shows whatever the viewer is part-way through, falling back to the
  // newest addition. Picking the first recently-added item keeps it stable between
  // renders rather than shuffling on every visit.
  const featured = useMemo(() => {
    if (!data) return null
    return data.continueWatching[0] ?? data.recentMovies[0] ?? data.recentShows[0] ?? null
  }, [data])

  // Fetch the featured item's overview so the hero has something to say. The list
  // endpoints deliberately omit it to keep grid payloads small.
  const { data: featuredDetail } = useQuery({
    queryKey: ['home-featured', featured?.kind, featured?.id],
    queryFn: () => api.get<MovieDetail>(`/movies/${featured!.id}`),
    enabled: featured?.kind === 'movie',
  })

  if (isLoading) return <HomeSkeleton />
  if (error) {
    return (
      <div className="p-8">
        <ErrorBox
          title="Could not load your library"
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

  if (libraries && libraries.length === 0) {
    return (
      <div className="p-8">
        <EmptyState
          title="No libraries yet"
          message={
            user?.isAdmin
              ? 'Add a folder of media and KINO will scan it and build your collection.'
              : 'An administrator needs to add a library before anything appears here.'
          }
          action={
            user?.isAdmin ? (
              <Link to="/settings" className="btn-primary">
                <SettingsIcon className="h-4 w-4" />
                Add a library
              </Link>
            ) : undefined
          }
        />
      </div>
    )
  }

  const empty =
    !data ||
    (data.continueWatching.length === 0 && data.recentMovies.length === 0 && data.recentShows.length === 0)

  if (empty) {
    return (
      <div className="p-8">
        <EmptyState
          title="Nothing here yet"
          message="Your libraries have not been scanned, or contain no recognised media."
          action={
            user?.isAdmin ? (
              <Link to="/settings" className="btn-primary">
                Run a scan
              </Link>
            ) : undefined
          }
        />
      </div>
    )
  }

  const showHero = prefs.heroOnHome && featured

  return (
    <div className="pb-16">
      {showHero && (
        <Hero
          item={{
            ...featured,
            overview: featuredDetail?.overview,
            rating: featuredDetail?.rating,
            genres: featuredDetail?.genres,
          }}
        />
      )}

      {/* Pull the first row up over the hero's fade so they overlap the way Netflix's do. */}
      <div className={showHero ? 'relative -mt-24 lg:-mt-32' : 'pt-6'}>
        {data.continueWatching.length > 0 && (
          <Row title="Continue Watching">
            {data.continueWatching.map((row) => (
              <MediaCard key={`cw-${row.id}`} item={toCard(row)} wide onChanged={refetch} />
            ))}
          </Row>
        )}

        {data.recentMovies.length > 0 && (
          <Row
            title="Recently Added Movies"
            action={
              <Link to="/movies" className="text-xs font-medium text-ink-400 transition-colors hover:text-white">
                See all →
              </Link>
            }
          >
            {data.recentMovies.map((row) => (
              <div key={`rm-${row.id}`} className="w-40 shrink-0 lg:w-44">
                <MediaCard item={toCard(row)} onChanged={refetch} />
              </div>
            ))}
          </Row>
        )}

        {data.recentShows.length > 0 && (
          <Row
            title="Recently Added Shows"
            action={
              <Link to="/shows" className="text-xs font-medium text-ink-400 transition-colors hover:text-white">
                See all →
              </Link>
            }
          >
            {data.recentShows.map((row) => (
              <div key={`rs-${row.id}`} className="w-40 shrink-0 lg:w-44">
                <MediaCard item={toCard(row)} onChanged={refetch} />
              </div>
            ))}
          </Row>
        )}
      </div>
    </div>
  )
}

function toCard(row: HomeRow): CardItem {
  return {
    id: row.id,
    kind: row.kind,
    title: row.title,
    subtitle: row.subtitle,
    posterId: row.posterId,
    backdropId: row.backdropId,
    year: row.year,
    progress: row.progress,
    durationSec: row.durationSec,
    available: true,
  }
}
