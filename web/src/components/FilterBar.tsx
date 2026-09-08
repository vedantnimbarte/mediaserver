import { useState } from 'react'
import { SlidersHorizontal, X } from 'lucide-react'

export interface Filters {
  genre: string
  year: string
  watched: 'all' | 'watched' | 'unwatched' | 'inprogress'
  resolution: 'all' | '4k' | '1080p' | '720p' | 'sd'
  sort: string
}

export const DEFAULT_FILTERS: Filters = {
  genre: '',
  year: '',
  watched: 'all',
  resolution: 'all',
  sort: 'title',
}

const watchedLabels: Record<Filters['watched'], string> = {
  all: 'All',
  watched: 'Watched',
  unwatched: 'Unwatched',
  inprogress: 'In progress',
}

const resolutionLabels: Record<Filters['resolution'], string> = {
  all: 'Any quality',
  '4k': '4K',
  '1080p': '1080p',
  '720p': '720p',
  sd: 'SD',
}

/**
 * FilterBar shows a compact row of quick filters, with the less-used controls behind
 * a toggle so the default view stays uncluttered.
 */
export function FilterBar({
  title,
  count,
  filters,
  onChange,
  genres,
  years,
  sortOptions,
  showResolution = true,
}: {
  title: string
  count: number
  filters: Filters
  onChange: (filters: Filters) => void
  genres: string[]
  years: number[]
  sortOptions: { value: string; label: string }[]
  showResolution?: boolean
}) {
  const [expanded, setExpanded] = useState(false)

  function set<K extends keyof Filters>(key: K, value: Filters[K]) {
    onChange({ ...filters, [key]: value })
  }

  const activeCount =
    (filters.genre ? 1 : 0) +
    (filters.year ? 1 : 0) +
    (filters.watched !== 'all' ? 1 : 0) +
    (filters.resolution !== 'all' ? 1 : 0)

  return (
    <div className="mb-6">
      <div className="flex flex-wrap items-center gap-3">
        <h1 className="text-2xl font-bold text-white">{title}</h1>
        <span className="rounded bg-ink-800 px-2 py-0.5 text-xs font-medium tabular-nums text-ink-300">
          {count}
        </span>

        <div className="ml-auto flex items-center gap-2">
          <button
            onClick={() => setExpanded((v) => !v)}
            className={`btn-sm gap-2 rounded border ${
              expanded || activeCount > 0
                ? 'border-white bg-white text-black'
                : 'border-ink-600 text-ink-200 hover:border-ink-400'
            }`}
          >
            <SlidersHorizontal className="h-3.5 w-3.5" />
            Filters
            {activeCount > 0 && (
              <span className="rounded-full bg-accent px-1.5 text-[10px] font-bold text-white">
                {activeCount}
              </span>
            )}
          </button>

          <select
            className="select w-auto py-1.5 text-xs"
            value={filters.sort}
            onChange={(e) => set('sort', e.target.value)}
            aria-label="Sort by"
          >
            {sortOptions.map((o) => (
              <option key={o.value} value={o.value}>
                {o.label}
              </option>
            ))}
          </select>
        </div>
      </div>

      {/* Active filters stay visible as removable chips even when the panel is
          collapsed, so a filtered view never looks like an empty library. */}
      {activeCount > 0 && (
        <div className="mt-3 flex flex-wrap items-center gap-2">
          {filters.genre && (
            <RemovableChip label={filters.genre} onRemove={() => set('genre', '')} />
          )}
          {filters.year && <RemovableChip label={filters.year} onRemove={() => set('year', '')} />}
          {filters.watched !== 'all' && (
            <RemovableChip label={watchedLabels[filters.watched]} onRemove={() => set('watched', 'all')} />
          )}
          {filters.resolution !== 'all' && (
            <RemovableChip
              label={resolutionLabels[filters.resolution]}
              onRemove={() => set('resolution', 'all')}
            />
          )}
          <button
            onClick={() => onChange({ ...DEFAULT_FILTERS, sort: filters.sort })}
            className="text-xs text-ink-400 underline-offset-2 transition-colors hover:text-white hover:underline"
          >
            Clear all
          </button>
        </div>
      )}

      {expanded && (
        <div className="mt-4 animate-slide-up space-y-4 rounded-lg border border-ink-700/60 bg-ink-850 p-4">
          <FilterGroup label="Watched">
            {(Object.keys(watchedLabels) as Filters['watched'][]).map((value) => (
              <button
                key={value}
                onClick={() => set('watched', value)}
                className={filters.watched === value ? 'chip-on' : 'chip-off'}
              >
                {watchedLabels[value]}
              </button>
            ))}
          </FilterGroup>

          {showResolution && (
            <FilterGroup label="Quality">
              {(Object.keys(resolutionLabels) as Filters['resolution'][]).map((value) => (
                <button
                  key={value}
                  onClick={() => set('resolution', value)}
                  className={filters.resolution === value ? 'chip-on' : 'chip-off'}
                >
                  {resolutionLabels[value]}
                </button>
              ))}
            </FilterGroup>
          )}

          {genres.length > 0 && (
            <FilterGroup label="Genre">
              <button
                onClick={() => set('genre', '')}
                className={filters.genre === '' ? 'chip-on' : 'chip-off'}
              >
                All
              </button>
              {genres.map((genre) => (
                <button
                  key={genre}
                  onClick={() => set('genre', genre)}
                  className={filters.genre === genre ? 'chip-on' : 'chip-off'}
                >
                  {genre}
                </button>
              ))}
            </FilterGroup>
          )}

          {years.length > 0 && (
            <FilterGroup label="Year">
              <select
                className="select w-auto py-1.5 text-xs"
                value={filters.year}
                onChange={(e) => set('year', e.target.value)}
                aria-label="Year"
              >
                <option value="">Any year</option>
                {years.map((year) => (
                  <option key={year} value={String(year)}>
                    {year}
                  </option>
                ))}
              </select>
            </FilterGroup>
          )}
        </div>
      )}
    </div>
  )
}

function FilterGroup({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <p className="mb-2 text-[11px] font-semibold uppercase tracking-wider text-ink-400">{label}</p>
      <div className="flex flex-wrap gap-2">{children}</div>
    </div>
  )
}

function RemovableChip({ label, onRemove }: { label: string; onRemove: () => void }) {
  return (
    <span className="chip border-ink-500 bg-ink-800 text-ink-200">
      {label}
      <button onClick={onRemove} aria-label={`Remove ${label} filter`} className="hover:text-white">
        <X className="h-3 w-3" />
      </button>
    </span>
  )
}

/** resolutionOf buckets a pixel height into the filter's categories. */
export function resolutionOf(height?: number): Filters['resolution'] {
  if (!height) return 'all'
  if (height >= 1700) return '4k'
  if (height >= 1000) return '1080p'
  if (height >= 700) return '720p'
  return 'sd'
}
