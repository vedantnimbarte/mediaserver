import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Clapperboard, FolderPlus, Image as ImageIcon, Loader2, Music, RefreshCw, Trash2, Tv } from 'lucide-react'

import { api, getToken, type Library, type LibraryType, type ScanProgress } from '../../lib/api'
import { useToast } from '../../components/Toast'
import { useConfirm } from '../../components/Modal'
import { Field, SectionCard } from '../../components/ui'
import { Skeleton } from '../../components/Skeleton'

const TYPE_META: Record<LibraryType, { label: string; icon: React.ReactNode; example: string }> = {
  movie: { label: 'Movies', icon: <Clapperboard className="h-4 w-4" />, example: 'D:\\Media\\Movies' },
  show: { label: 'TV Shows', icon: <Tv className="h-4 w-4" />, example: 'D:\\Media\\TV' },
  music: { label: 'Music', icon: <Music className="h-4 w-4" />, example: 'D:\\Media\\Music' },
  photo: { label: 'Photos', icon: <ImageIcon className="h-4 w-4" />, example: 'D:\\Media\\Photos' },
}

const PHASE_LABEL: Record<string, string> = {
  walking: 'Finding files',
  probing: 'Analysing media',
  metadata: 'Fetching metadata',
  cleaning: 'Tidying up',
  done: 'Done',
  failed: 'Failed',
}

export default function LibrariesTab() {
  const queryClient = useQueryClient()
  const toast = useToast()
  const confirm = useConfirm()
  const [adding, setAdding] = useState(false)
  const [progress, setProgress] = useState<Record<string, ScanProgress>>({})

  const { data: libraries, isLoading } = useQuery({
    queryKey: ['libraries'],
    queryFn: () => api.get<Library[]>('/libraries'),
  })

  // EventSource cannot set headers, so the token rides in the query string the same
  // way it does for media URLs.
  useEffect(() => {
    const source = new EventSource(`/api/scan/progress?api_key=${encodeURIComponent(getToken() ?? '')}`)

    source.onmessage = (event) => {
      try {
        const update: ScanProgress = JSON.parse(event.data)
        setProgress((prev) => ({ ...prev, [update.libraryId]: update }))

        if (update.finished) {
          if (update.error) {
            toast.error(`Scan of ${update.libraryName} failed`, update.error)
          } else {
            toast.success(
              `${update.libraryName} scanned`,
              `${update.added} added, ${update.updated} updated, ${update.removed} removed.`,
            )
          }
          queryClient.invalidateQueries({ queryKey: ['libraries'] })
          queryClient.invalidateQueries({ queryKey: ['home'] })
          queryClient.invalidateQueries({ queryKey: ['movies'] })
          queryClient.invalidateQueries({ queryKey: ['shows'] })
          queryClient.invalidateQueries({ queryKey: ['maintenance'] })
        }
      } catch {
        /* a malformed event is not worth interrupting the page for */
      }
    }

    return () => source.close()
  }, [queryClient, toast])

  const scan = useMutation({
    mutationFn: (id: string) => api.post(`/libraries/${id}/scan`),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['libraries'] }),
    onError: (err: any) => toast.error('Could not start the scan', err?.message),
  })

  const remove = useMutation({
    mutationFn: (id: string) => api.del(`/libraries/${id}`),
    onSuccess: () => {
      toast.success('Library removed', 'Your media files were not touched.')
      queryClient.invalidateQueries({ queryKey: ['libraries'] })
    },
    onError: (err: any) => toast.error('Could not remove the library', err?.message),
  })

  if (isLoading) {
    return (
      <div className="space-y-3">
        <Skeleton className="h-28 w-full rounded-lg" />
        <Skeleton className="h-28 w-full rounded-lg" />
      </div>
    )
  }

  return (
    <div className="space-y-4">
      {libraries?.map((lib) => {
        const p = progress[lib.id]
        const running = Boolean(p && !p.finished) || lib.scanning
        const meta = TYPE_META[lib.type]

        return (
          <div key={lib.id} className="panel px-5 py-4">
            <div className="flex flex-wrap items-start gap-4">
              <div className="mt-0.5 shrink-0 text-ink-400">{meta?.icon}</div>

              <div className="min-w-0 flex-1">
                <h3 className="font-semibold text-white">{lib.name}</h3>
                <p className="mt-0.5 text-xs text-ink-400">
                  {meta?.label} · {lib.itemCount} items
                  {lib.lastScanAt && ` · scanned ${new Date(lib.lastScanAt).toLocaleString()}`}
                </p>
                <ul className="mt-2 space-y-0.5">
                  {lib.paths.map((path) => (
                    <li key={path} className="truncate font-mono text-xs text-ink-500">
                      {path}
                    </li>
                  ))}
                </ul>
              </div>

              <div className="flex gap-2">
                <button className="btn-sm btn-outline" onClick={() => scan.mutate(lib.id)} disabled={running}>
                  {running ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <RefreshCw className="h-3.5 w-3.5" />}
                  {running ? 'Scanning' : 'Scan'}
                </button>
                <button
                  className="btn-sm btn-danger"
                  aria-label={`Remove ${lib.name}`}
                  onClick={async () => {
                    const ok = await confirm({
                      title: `Remove “${lib.name}”?`,
                      message:
                        'The library and everything scanned from it is removed from KINO. The files on your disk are not deleted.',
                      confirmLabel: 'Remove library',
                      destructive: true,
                    })
                    if (ok) remove.mutate(lib.id)
                  }}
                >
                  <Trash2 className="h-3.5 w-3.5" />
                </button>
              </div>
            </div>

            {p && !p.finished && (
              <div className="mt-4">
                <div className="mb-1.5 flex justify-between gap-4 text-xs">
                  <span className="truncate text-ink-300">
                    {PHASE_LABEL[p.phase] ?? p.phase}
                    {p.current && <span className="text-ink-500"> · {p.current}</span>}
                  </span>
                  {p.total > 0 && (
                    <span className="shrink-0 tabular-nums text-ink-400">
                      {p.done} / {p.total}
                    </span>
                  )}
                </div>
                <div className="h-1.5 overflow-hidden rounded-full bg-ink-700">
                  <div
                    className="h-full rounded-full bg-accent transition-all duration-300"
                    style={{ width: p.total > 0 ? `${(p.done / p.total) * 100}%` : '20%' }}
                  />
                </div>
              </div>
            )}
          </div>
        )
      })}

      {adding ? (
        <AddLibraryForm
          onDone={() => {
            setAdding(false)
            queryClient.invalidateQueries({ queryKey: ['libraries'] })
          }}
          onCancel={() => setAdding(false)}
        />
      ) : (
        <button className="btn-primary" onClick={() => setAdding(true)}>
          <FolderPlus className="h-4 w-4" />
          Add library
        </button>
      )}
    </div>
  )
}

function AddLibraryForm({ onDone, onCancel }: { onDone: () => void; onCancel: () => void }) {
  const toast = useToast()
  const [name, setName] = useState('')
  const [type, setType] = useState<LibraryType>('movie')
  const [path, setPath] = useState('')

  const create = useMutation({
    mutationFn: () => api.post<Library>('/libraries', { name, type, paths: [path] }),
    onSuccess: async (lib) => {
      // Scanning straight away is what the user wants next in every case; an empty
      // library is never the intended end state.
      await api.post(`/libraries/${lib.id}/scan`).catch(() => {})
      toast.success('Library added', 'Scanning has started.')
      onDone()
    },
    onError: (err: any) => toast.error('Could not create the library', err?.message),
  })

  return (
    <SectionCard title="New library" description="Point KINO at a folder on the machine running the server.">
      <form
        className="space-y-4"
        onSubmit={(e) => {
          e.preventDefault()
          create.mutate()
        }}
      >
        <Field label="Type">
          <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
            {(Object.keys(TYPE_META) as LibraryType[]).map((value) => (
              <button
                key={value}
                type="button"
                onClick={() => setType(value)}
                className={`flex items-center gap-2 rounded-lg border px-3 py-2.5 text-sm transition-colors ${
                  type === value ? 'border-white bg-ink-700 text-white' : 'border-ink-600 text-ink-300 hover:border-ink-400'
                }`}
              >
                {TYPE_META[value].icon}
                {TYPE_META[value].label}
              </button>
            ))}
          </div>
        </Field>

        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="Name" htmlFor="lib-name">
            <input
              id="lib-name"
              className="input"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={TYPE_META[type].label}
              required
            />
          </Field>

          <Field label="Folder" htmlFor="lib-path" hint="The full path as the server sees it.">
            <input
              id="lib-path"
              className="input font-mono text-xs"
              value={path}
              onChange={(e) => setPath(e.target.value)}
              placeholder={TYPE_META[type].example}
              required
            />
          </Field>
        </div>

        <div className="flex gap-2">
          <button type="submit" className="btn-primary" disabled={create.isPending}>
            {create.isPending ? 'Creating…' : 'Create and scan'}
          </button>
          <button type="button" className="btn-outline" onClick={onCancel}>
            Cancel
          </button>
        </div>
      </form>
    </SectionCard>
  )
}
