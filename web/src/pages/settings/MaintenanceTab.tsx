import { useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Download, HardDrive, RefreshCw, Trash2, Upload } from 'lucide-react'

import { api, authedUrl, type MaintenanceStats } from '../../lib/api'
import { useToast } from '../../components/Toast'
import { useConfirm } from '../../components/Modal'
import { SectionCard, StatTile, formatBytes } from '../../components/ui'
import { Skeleton } from '../../components/Skeleton'

export default function MaintenanceTab() {
  const queryClient = useQueryClient()
  const toast = useToast()
  const confirm = useConfirm()
  const fileRef = useRef<HTMLInputElement>(null)
  const [logs, setLogs] = useState<string[]>([])
  const [logsOpen, setLogsOpen] = useState(false)

  const { data: stats, isLoading } = useQuery({
    queryKey: ['maintenance'],
    queryFn: () => api.get<MaintenanceStats>('/maintenance/stats'),
    // Active scan and stream counts go stale quickly while something is running.
    refetchInterval: 10_000,
  })

  const clearImages = useMutation({
    mutationFn: () => api.post<{ freedBytes: number }>('/maintenance/cache/images/clear'),
    onSuccess: (res) => {
      toast.success('Image cache cleared', `Freed ${formatBytes(res.freedBytes)}. Artwork is refetched as needed.`)
      queryClient.invalidateQueries({ queryKey: ['maintenance'] })
    },
    onError: (err: any) => toast.error('Could not clear the cache', err?.message),
  })

  const clearSubtitles = useMutation({
    mutationFn: () => api.post<{ freedBytes: number }>('/maintenance/cache/subtitles/clear'),
    onSuccess: (res) => {
      toast.success('Subtitle cache cleared', `Freed ${formatBytes(res.freedBytes)}.`)
      queryClient.invalidateQueries({ queryKey: ['maintenance'] })
    },
    onError: (err: any) => toast.error('Could not clear the cache', err?.message),
  })

  const loadLogs = useMutation({
    mutationFn: () => api.get<{ lines: string[] }>('/maintenance/logs?limit=300'),
    onSuccess: (res) => {
      setLogs(res.lines)
      setLogsOpen(true)
    },
    onError: (err: any) => toast.error('Could not load the logs', err?.message),
  })

  const restore = useMutation({
    mutationFn: async (file: File) => {
      const text = await file.text()
      // Send the parsed object so a malformed file fails here, with a clear message,
      // rather than as an opaque server error.
      let payload: unknown
      try {
        payload = JSON.parse(text)
      } catch {
        throw new Error('That file is not valid JSON.')
      }
      return api.post<{ movies: number; shows: number }>('/maintenance/restore', payload)
    },
    onSuccess: (res) => {
      toast.success('Backup restored', `${res.movies} movies and ${res.shows} shows loaded.`)
      queryClient.invalidateQueries()
    },
    onError: (err: any) => toast.error('Restore failed', err?.message),
  })

  async function onRestoreFile(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0]
    e.target.value = '' // let the same file be chosen again after a failure
    if (!file) return

    const ok = await confirm({
      title: 'Restore this backup?',
      message:
        'Your current libraries, metadata and watch history are replaced by the contents of the backup. Media files on disk are not touched.',
      confirmLabel: 'Restore',
      destructive: true,
    })
    if (ok) restore.mutate(file)
  }

  if (isLoading || !stats) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-32 w-full rounded-lg" />
        <Skeleton className="h-48 w-full rounded-lg" />
      </div>
    )
  }

  const cacheTotal = stats.imageCache.bytes + stats.subtitleCache.bytes + stats.transcodeTemp.bytes

  return (
    <div className="space-y-4">
      <SectionCard title="At a glance" description={`Uptime ${stats.uptime}`}>
        <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
          <StatTile label="Active streams" value={String(stats.activeStreams)} />
          <StatTile label="Running scans" value={String(stats.activeScans)} />
          <StatTile label="Database" value={formatBytes(stats.databaseBytes)} hint="JSON on disk" />
          <StatTile label="Caches" value={formatBytes(cacheTotal)} hint="Artwork, subtitles, temp" />
        </div>
      </SectionCard>

      <SectionCard title="Storage by library" description="Sizes come from what the last scan recorded.">
        {stats.libraries.length === 0 ? (
          <p className="text-sm text-ink-400">No libraries yet.</p>
        ) : (
          <div className="space-y-3">
            {stats.libraries.map((lib) => {
              const largest = Math.max(...stats.libraries.map((l) => l.bytes), 1)
              return (
                <div key={lib.libraryId}>
                  <div className="mb-1 flex items-baseline justify-between gap-4 text-sm">
                    <span className="truncate font-medium text-ink-100">{lib.libraryName}</span>
                    <span className="shrink-0 tabular-nums text-ink-400">
                      {lib.items} items · {formatBytes(lib.bytes)}
                    </span>
                  </div>
                  <div className="h-1.5 overflow-hidden rounded-full bg-ink-700">
                    <div className="h-full rounded-full bg-accent" style={{ width: `${(lib.bytes / largest) * 100}%` }} />
                  </div>
                </div>
              )
            })}
          </div>
        )}
      </SectionCard>

      <SectionCard
        title="Caches"
        description="Everything here is regenerated on demand, so clearing it only costs a little time."
      >
        <div className="space-y-3">
          <CacheRow
            icon={<HardDrive className="h-4 w-4" />}
            name="Artwork"
            stat={stats.imageCache}
            onClear={() => clearImages.mutate()}
            busy={clearImages.isPending}
          />
          <CacheRow
            icon={<HardDrive className="h-4 w-4" />}
            name="Converted subtitles"
            stat={stats.subtitleCache}
            onClear={() => clearSubtitles.mutate()}
            busy={clearSubtitles.isPending}
          />
          <div className="flex items-center gap-3 rounded-lg border border-ink-700/60 bg-ink-800/50 px-4 py-3">
            <HardDrive className="h-4 w-4 shrink-0 text-ink-400" />
            <div className="min-w-0 flex-1">
              <p className="text-sm font-medium text-ink-100">Transcode scratch space</p>
              <p className="truncate text-xs text-ink-400">
                {stats.transcodeTemp.files} files · {formatBytes(stats.transcodeTemp.bytes)}
              </p>
            </div>
            <span className="shrink-0 text-xs text-ink-500">Cleared automatically</span>
          </div>
        </div>
      </SectionCard>

      <SectionCard title="Backup" description="Export your libraries, metadata and watch history as one JSON file.">
        <div className="flex flex-wrap gap-2">
          <a className="btn-outline" href={authedUrl('/api/maintenance/backup')} download>
            <Download className="h-4 w-4" />
            Download backup
          </a>
          <button className="btn-outline" onClick={() => fileRef.current?.click()} disabled={restore.isPending}>
            <Upload className="h-4 w-4" />
            {restore.isPending ? 'Restoring…' : 'Restore from file'}
          </button>
          <input ref={fileRef} type="file" accept="application/json,.json" onChange={onRestoreFile} className="hidden" />
        </div>
        <p className="hint">
          User accounts are deliberately excluded, so password hashes never leave the server.
        </p>
      </SectionCard>

      <SectionCard
        title="Server log"
        description="The most recent activity, useful when something did not behave."
        action={
          <button className="btn-sm btn-outline" onClick={() => loadLogs.mutate()} disabled={loadLogs.isPending}>
            <RefreshCw className={`h-3.5 w-3.5 ${loadLogs.isPending ? 'animate-spin' : ''}`} />
            {logsOpen ? 'Refresh' : 'Load'}
          </button>
        }
      >
        {logsOpen ? (
          <div className="max-h-80 overflow-auto rounded border border-ink-700 bg-black/50 p-3">
            {logs.length === 0 ? (
              <p className="text-sm text-ink-400">Nothing logged yet.</p>
            ) : (
              <pre className="whitespace-pre-wrap break-words font-mono text-[11px] leading-relaxed text-ink-300">
                {logs.join('\n')}
              </pre>
            )}
          </div>
        ) : (
          <p className="text-sm text-ink-400">Load the log to see recent scans, playback and warnings.</p>
        )}
      </SectionCard>
    </div>
  )
}

function CacheRow({
  icon,
  name,
  stat,
  onClear,
  busy,
}: {
  icon: React.ReactNode
  name: string
  stat: { files: number; bytes: number }
  onClear: () => void
  busy: boolean
}) {
  return (
    <div className="flex items-center gap-3 rounded-lg border border-ink-700/60 bg-ink-800/50 px-4 py-3">
      <span className="shrink-0 text-ink-400">{icon}</span>
      <div className="min-w-0 flex-1">
        <p className="text-sm font-medium text-ink-100">{name}</p>
        <p className="truncate text-xs text-ink-400">
          {stat.files} files · {formatBytes(stat.bytes)}
        </p>
      </div>
      <button className="btn-sm btn-outline shrink-0" onClick={onClear} disabled={busy || stat.files === 0}>
        <Trash2 className="h-3.5 w-3.5" />
        Clear
      </button>
    </div>
  )
}
