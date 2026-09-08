import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { CheckCircle2, RefreshCw } from 'lucide-react'

import { api, type Settings } from '../../lib/api'
import { useAuth } from '../../lib/auth'
import { useToast } from '../../components/Toast'
import { useConfirm } from '../../components/Modal'
import { Field, SectionCard, StatTile, Toggle } from '../../components/ui'
import { Skeleton } from '../../components/Skeleton'

const LANGUAGES = [
  { value: 'en-US', label: 'English' },
  { value: 'hi-IN', label: 'Hindi' },
  { value: 'es-ES', label: 'Spanish' },
  { value: 'fr-FR', label: 'French' },
  { value: 'de-DE', label: 'German' },
  { value: 'it-IT', label: 'Italian' },
  { value: 'pt-BR', label: 'Portuguese (Brazil)' },
  { value: 'ja-JP', label: 'Japanese' },
  { value: 'ko-KR', label: 'Korean' },
  { value: 'zh-CN', label: 'Chinese' },
]

export default function ServerTab() {
  const queryClient = useQueryClient()
  const { refreshServerInfo } = useAuth()
  const toast = useToast()
  const confirm = useConfirm()
  const [apiKey, setApiKey] = useState('')

  const { data: settings, isLoading } = useQuery({
    queryKey: ['settings'],
    queryFn: () => api.get<Settings>('/settings'),
  })

  const save = useMutation({
    mutationFn: (body: Record<string, unknown>) => api.put<{ needsRestart: string[] }>('/settings', body),
    onSuccess: async (res) => {
      if (res.needsRestart?.length) {
        toast.info('Saved', `Restart the server to apply: ${res.needsRestart.join(', ')}.`)
      } else {
        toast.success('Saved')
      }
      setApiKey('')
      queryClient.invalidateQueries({ queryKey: ['settings'] })
      await refreshServerInfo()
    },
    onError: (err: any) => toast.error('Could not save', err?.message),
  })

  const verify = useMutation({
    mutationFn: () => api.post('/settings/verify-metadata-key'),
    onSuccess: () => toast.success('The TMDb key works', 'Posters and descriptions will be fetched on the next scan.'),
    onError: (err: any) => toast.error('Key check failed', err?.message),
  })

  const refreshMetadata = useMutation({
    mutationFn: (unlock: boolean) => api.post(`/metadata/refresh${unlock ? '?unlock=1' : ''}`),
    onSuccess: () => toast.info('Metadata queued', 'Run a scan to fetch it.'),
    onError: (err: any) => toast.error('Could not queue a refresh', err?.message),
  })

  if (isLoading || !settings) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-40 w-full rounded-lg" />
        <Skeleton className="h-40 w-full rounded-lg" />
      </div>
    )
  }

  return (
    <div className="space-y-4">
      <SectionCard
        title="Metadata provider"
        description="A free TMDb key gives you posters, descriptions, cast and episode details."
      >
        <div className="flex flex-wrap items-end gap-2">
          <div className="min-w-56 flex-1">
            <Field label="TMDb API key" htmlFor="tmdb">
              <input
                id="tmdb"
                className="input font-mono text-xs"
                value={apiKey}
                onChange={(e) => setApiKey(e.target.value)}
                placeholder={settings.hasMetadataKey ? '•••••••••••• (a key is saved)' : 'Paste your key'}
                autoComplete="off"
              />
            </Field>
          </div>
          <button
            className="btn-primary"
            onClick={() => save.mutate({ tmdbApiKey: apiKey })}
            disabled={!apiKey || save.isPending}
          >
            Save key
          </button>
          <button
            className="btn-outline"
            onClick={() => verify.mutate()}
            disabled={!settings.hasMetadataKey || verify.isPending}
          >
            {verify.isSuccess ? <CheckCircle2 className="h-4 w-4 text-emerald-400" /> : null}
            Test
          </button>
        </div>

        <div className="mt-4 grid gap-4 sm:grid-cols-2">
          <Field label="Metadata language" htmlFor="metaLang">
            <select
              id="metaLang"
              className="select"
              value={settings.metadataLang}
              onChange={(e) => save.mutate({ metadataLang: e.target.value })}
            >
              {LANGUAGES.map((l) => (
                <option key={l.value} value={l.value}>
                  {l.label}
                </option>
              ))}
            </select>
          </Field>
        </div>

        <div className="mt-4 flex flex-wrap gap-2">
          <button className="btn-outline" onClick={() => refreshMetadata.mutate(false)}>
            <RefreshCw className="h-4 w-4" />
            Refresh metadata
          </button>
          <button
            className="btn-outline"
            onClick={async () => {
              const ok = await confirm({
                title: 'Clear all matches and start over?',
                message:
                  'Every item is unmatched, including ones you corrected by hand, and will be looked up again on the next scan.',
                confirmLabel: 'Clear matches',
                destructive: true,
              })
              if (ok) refreshMetadata.mutate(true)
            }}
          >
            Clear all matches
          </button>
        </div>
      </SectionCard>

      <SectionCard title="Scanning" description="How KINO keeps up with changes on disk.">
        <Toggle
          label="Scan every library on startup"
          description="Useful if files change while the server is off. Adds a little time to boot."
          checked={settings.scanOnStart}
          onChange={(v) => save.mutate({ scanOnStart: v })}
        />
      </SectionCard>

      <SectionCard title="Network" description="Changing the port needs a restart.">
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="Port" htmlFor="port" hint="Other devices reach KINO at http://<server-ip>:<port>.">
            <input
              id="port"
              type="number"
              min={1}
              max={65535}
              className="input"
              defaultValue={settings.port}
              onBlur={(e) => {
                const value = Number(e.target.value)
                if (value !== settings.port) save.mutate({ port: value })
              }}
            />
          </Field>
        </div>
      </SectionCard>

      <SectionCard title="Paths" description="Where things live on this machine.">
        <div className="grid gap-3 sm:grid-cols-2">
          <StatTile label="Data directory" value={settings.dataDir} />
          <StatTile
            label="ffmpeg"
            value={settings.ffmpegReady ? 'Found' : 'Missing'}
            hint={settings.ffmpegVersion ?? 'Scanning and playback need it'}
          />
        </div>

        {!settings.ffmpegReady && (
          <div className="mt-4">
            <Field
              label="ffmpeg folder"
              htmlFor="ffmpegpath"
              hint="Point this at the bin folder of a portable ffmpeg build if it is not on your PATH."
            >
              <input
                id="ffmpegpath"
                className="input font-mono text-xs"
                defaultValue={settings.ffmpegPath}
                placeholder="C:\ffmpeg\bin"
                onBlur={(e) => {
                  if (e.target.value !== settings.ffmpegPath) save.mutate({ ffmpegPath: e.target.value })
                }}
              />
            </Field>
          </div>
        )}
      </SectionCard>
    </div>
  )
}
