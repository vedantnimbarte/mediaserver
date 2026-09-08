import { AlertCircle, Film, Loader2 } from 'lucide-react'
import { imageUrl } from '../lib/api'

export function Spinner({ label }: { label?: string }) {
  return (
    <div className="flex flex-col items-center justify-center gap-3 py-16 text-ink-400">
      <Loader2 className="h-6 w-6 animate-spin" />
      {label && <p className="text-sm">{label}</p>}
    </div>
  )
}

export function ErrorBox({
  title,
  message,
  action,
}: {
  title: string
  message?: string
  action?: React.ReactNode
}) {
  return (
    <div className="mx-auto max-w-lg rounded-lg border border-red-900/50 bg-red-950/25 p-6 text-center">
      <AlertCircle className="mx-auto mb-3 h-7 w-7 text-red-400" />
      <h2 className="mb-1 font-medium text-red-100">{title}</h2>
      {message && <p className="text-sm text-red-200/70">{message}</p>}
      {action && <div className="mt-4 flex justify-center">{action}</div>}
    </div>
  )
}

export function EmptyState({
  icon,
  title,
  message,
  action,
}: {
  icon?: React.ReactNode
  title: string
  message?: string
  action?: React.ReactNode
}) {
  return (
    <div className="flex flex-col items-center justify-center py-24 text-center">
      <div className="mb-4 text-ink-600">{icon ?? <Film className="h-14 w-14" />}</div>
      <h2 className="mb-2 text-xl font-semibold text-white">{title}</h2>
      {message && <p className="max-w-md text-sm leading-relaxed text-ink-400">{message}</p>}
      {action && <div className="mt-6">{action}</div>}
    </div>
  )
}

/** Poster renders artwork with a graceful placeholder when there is none. */
export function Poster({
  imageId,
  alt,
  width = 342,
  className = '',
}: {
  imageId?: string
  alt: string
  width?: number
  className?: string
}) {
  const src = imageUrl(imageId, width)
  if (!src) {
    return (
      <div className={`flex items-center justify-center bg-ink-800 text-ink-600 ${className}`} aria-label={alt}>
        <Film className="h-8 w-8" />
      </div>
    )
  }
  return (
    <img
      src={src}
      alt={alt}
      loading="lazy"
      onError={(e) => {
        e.currentTarget.style.visibility = 'hidden'
      }}
      className={className}
    />
  )
}

export function ProgressBar({ value }: { value: number }) {
  if (value <= 0) return null
  return (
    <div className="absolute inset-x-0 bottom-0 h-1 bg-black/70">
      <div className="h-full bg-accent" style={{ width: `${Math.min(100, value * 100)}%` }} />
    </div>
  )
}

/** Toggle is the switch used throughout Settings. */
export function Toggle({
  checked,
  onChange,
  label,
  description,
  disabled,
}: {
  checked: boolean
  onChange: (value: boolean) => void
  label: string
  description?: string
  disabled?: boolean
}) {
  return (
    <div className="flex items-start justify-between gap-6 py-3">
      <div className="min-w-0">
        <p className="text-sm font-medium text-ink-100">{label}</p>
        {description && <p className="mt-0.5 text-xs leading-relaxed text-ink-400">{description}</p>}
      </div>
      <button
        type="button"
        role="switch"
        aria-checked={checked}
        aria-label={label}
        disabled={disabled}
        onClick={() => onChange(!checked)}
        className={`switch shrink-0 ${checked ? 'switch-on' : 'switch-off'} ${disabled ? 'opacity-40' : ''}`}
      >
        <span className={`switch-knob ${checked ? 'translate-x-5' : 'translate-x-0.5'}`} />
      </button>
    </div>
  )
}

/** Field wraps a labelled control with an optional hint. */
export function Field({
  label,
  hint,
  htmlFor,
  children,
}: {
  label: string
  hint?: string
  htmlFor?: string
  children: React.ReactNode
}) {
  return (
    <div>
      <label className="label" htmlFor={htmlFor}>
        {label}
      </label>
      {children}
      {hint && <p className="hint">{hint}</p>}
    </div>
  )
}

/** SectionCard groups related settings under a heading. */
export function SectionCard({
  title,
  description,
  children,
  action,
}: {
  title: string
  description?: string
  children: React.ReactNode
  action?: React.ReactNode
}) {
  return (
    <section className="panel">
      <header className="flex items-start justify-between gap-4 border-b border-ink-700/60 px-5 py-4">
        <div>
          <h3 className="font-semibold text-white">{title}</h3>
          {description && <p className="mt-0.5 text-xs leading-relaxed text-ink-400">{description}</p>}
        </div>
        {action}
      </header>
      <div className="px-5 py-4">{children}</div>
    </section>
  )
}

/** StatTile is the compact number readout used on the maintenance panel. */
export function StatTile({ label, value, hint }: { label: string; value: string; hint?: string }) {
  return (
    <div className="rounded-lg border border-ink-700/60 bg-ink-800/60 px-4 py-3">
      <p className="text-xs font-medium uppercase tracking-wider text-ink-400">{label}</p>
      <p className="mt-1 text-lg font-semibold tabular-nums text-white">{value}</p>
      {hint && <p className="mt-0.5 text-xs text-ink-400">{hint}</p>}
    </div>
  )
}

/** formatDuration renders seconds as "1h 42m" or "8m". */
export function formatDuration(seconds?: number): string {
  if (!seconds || seconds <= 0) return ''
  const total = Math.round(seconds)
  const h = Math.floor(total / 3600)
  const m = Math.round((total % 3600) / 60)
  if (h > 0) return m > 0 ? `${h}h ${m}m` : `${h}h`
  return `${m}m`
}

/** formatTime renders seconds as a playback clock: 1:02:03 or 2:03. */
export function formatTime(seconds: number): string {
  if (!isFinite(seconds) || seconds < 0) seconds = 0
  const total = Math.floor(seconds)
  const h = Math.floor(total / 3600)
  const m = Math.floor((total % 3600) / 60)
  const s = total % 60
  const pad = (n: number) => String(n).padStart(2, '0')
  return h > 0 ? `${h}:${pad(m)}:${pad(s)}` : `${m}:${pad(s)}`
}

export function formatBytes(bytes?: number): string {
  if (!bytes) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let value = bytes
  let unit = 0
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024
    unit++
  }
  return `${value.toFixed(value >= 10 || unit === 0 ? 0 : 1)} ${units[unit]}`
}
