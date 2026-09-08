/**
 * Content-shaped loading placeholders.
 *
 * These deliberately mirror the layout of the real content, so the page does not
 * reflow when data arrives — a centred spinner tells you nothing and then everything
 * jumps into place.
 */

export function Skeleton({ className = '' }: { className?: string }) {
  return <div className={`skeleton ${className}`} aria-hidden="true" />
}

export function PosterSkeleton({ showTitle = true }: { showTitle?: boolean }) {
  return (
    <div>
      <Skeleton className="aspect-[2/3] w-full rounded" />
      {showTitle && (
        <>
          <Skeleton className="mt-2 h-3 w-3/4" />
          <Skeleton className="mt-1.5 h-2.5 w-1/3" />
        </>
      )}
    </div>
  )
}

export function GridSkeleton({ count = 18, showTitle = true }: { count?: number; showTitle?: boolean }) {
  return (
    <div className="poster-grid" aria-busy="true" aria-label="Loading">
      {Array.from({ length: count }).map((_, i) => (
        <PosterSkeleton key={i} showTitle={showTitle} />
      ))}
    </div>
  )
}

export function RowSkeleton({ wide = false }: { wide?: boolean }) {
  return (
    <div className="mb-8" aria-busy="true">
      <Skeleton className="mb-4 ml-4 h-5 w-48 lg:ml-12" />
      <div className="flex gap-2 px-4 lg:px-12">
        {Array.from({ length: 7 }).map((_, i) => (
          <div key={i} className={wide ? 'w-64 shrink-0' : 'w-40 shrink-0'}>
            <Skeleton className={wide ? 'aspect-video w-full rounded' : 'aspect-[2/3] w-full rounded'} />
          </div>
        ))}
      </div>
    </div>
  )
}

export function HeroSkeleton() {
  return (
    <div className="relative h-[56vh] min-h-[380px] w-full" aria-busy="true">
      <Skeleton className="h-full w-full rounded-none" />
      <div className="absolute inset-x-0 bottom-0 px-4 pb-16 lg:px-12">
        <Skeleton className="h-10 w-2/3 max-w-lg" />
        <Skeleton className="mt-4 h-3 w-1/3 max-w-xs" />
        <Skeleton className="mt-4 h-3 w-full max-w-xl" />
        <Skeleton className="mt-2 h-3 w-4/5 max-w-lg" />
        <div className="mt-6 flex gap-3">
          <Skeleton className="h-11 w-28 rounded" />
          <Skeleton className="h-11 w-32 rounded" />
        </div>
      </div>
    </div>
  )
}

export function HomeSkeleton() {
  return (
    <div>
      <HeroSkeleton />
      <div className="-mt-12 relative">
        <RowSkeleton wide />
        <RowSkeleton />
        <RowSkeleton />
      </div>
    </div>
  )
}

export function DetailSkeleton() {
  return (
    <div aria-busy="true">
      <Skeleton className="h-72 w-full rounded-none lg:h-96" />
      <div className="relative -mt-28 px-4 lg:px-12">
        <div className="flex flex-col gap-6 sm:flex-row">
          <Skeleton className="aspect-[2/3] w-40 shrink-0 rounded-lg sm:w-52" />
          <div className="flex-1 pt-6">
            <Skeleton className="h-9 w-2/3 max-w-md" />
            <Skeleton className="mt-3 h-3 w-1/2 max-w-xs" />
            <Skeleton className="mt-6 h-3 w-full max-w-2xl" />
            <Skeleton className="mt-2 h-3 w-5/6 max-w-xl" />
            <Skeleton className="mt-2 h-3 w-3/4 max-w-lg" />
            <div className="mt-6 flex gap-3">
              <Skeleton className="h-11 w-28 rounded" />
              <Skeleton className="h-11 w-32 rounded" />
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}

export function ListSkeleton({ rows = 6 }: { rows?: number }) {
  return (
    <div className="space-y-2" aria-busy="true">
      {Array.from({ length: rows }).map((_, i) => (
        <div key={i} className="flex gap-4 rounded-lg border border-ink-700/60 bg-ink-850 p-3">
          <Skeleton className="aspect-video w-32 shrink-0 rounded sm:w-40" />
          <div className="flex-1 py-1">
            <Skeleton className="h-4 w-2/5" />
            <Skeleton className="mt-2 h-2.5 w-1/4" />
            <Skeleton className="mt-3 h-2.5 w-full" />
            <Skeleton className="mt-1.5 h-2.5 w-4/5" />
          </div>
        </div>
      ))}
    </div>
  )
}
