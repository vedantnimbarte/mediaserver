import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Cpu, Zap } from 'lucide-react'

import { api, type SessionInfo, type Settings } from '../../lib/api'
import { useToast } from '../../components/Toast'
import { Field, SectionCard, StatTile, Toggle } from '../../components/ui'
import { Skeleton } from '../../components/Skeleton'

/** The rungs the server may build, so overrides can be offered without a live session. */
const RUNGS = ['2160p', '1080p', '720p', '480p', '360p']

export default function TranscodingTab() {
  const queryClient = useQueryClient()
  const toast = useToast()
  const [overrides, setOverrides] = useState<Record<string, string> | null>(null)

  const { data: settings, isLoading } = useQuery({
    queryKey: ['settings'],
    queryFn: () => api.get<Settings>('/settings'),
  })

  const { data: sessions } = useQuery({
    queryKey: ['sessions'],
    queryFn: () => api.get<SessionInfo[]>('/playback/sessions'),
    refetchInterval: 5_000,
  })

  const save = useMutation({
    mutationFn: (body: Record<string, unknown>) =>
      api.put<{ needsRestart: string[] }>('/settings', body),
    onSuccess: (res) => {
      if (res.needsRestart?.length) {
        toast.info('Saved', `Restart the server to apply: ${res.needsRestart.join(', ')}.`)
      } else {
        toast.success('Saved')
      }
      queryClient.invalidateQueries({ queryKey: ['settings'] })
    },
    onError: (err: any) => toast.error('Could not save', err?.message),
  })

  if (isLoading || !settings) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-40 w-full rounded-lg" />
        <Skeleton className="h-64 w-full rounded-lg" />
      </div>
    )
  }

  // Seed the override editor from the server the first time it renders.
  const overrideValues =
    overrides ??
    Object.fromEntries(RUNGS.map((rung) => [rung, settings.bitrateOverrides?.[rung] ? String(Math.round(settings.bitrateOverrides[rung] / 1000)) : '']))

  function setOverride(rung: string, value: string) {
    setOverrides({ ...overrideValues, [rung]: value })
  }

  function saveOverrides() {
    const payload: Record<string, number> = {}
    for (const [rung, value] of Object.entries(overrideValues)) {
      const kbps = Number(value)
      if (value.trim() !== '' && Number.isFinite(kbps) && kbps > 0) {
        payload[rung] = Math.round(kbps * 1000)
      }
    }
    save.mutate({ bitrateOverrides: payload })
  }

  return (
    <div className="space-y-4">
      <SectionCard title="Encoder" description="What does the work when a file has to be converted.">
        <div className="mb-4 grid grid-cols-2 gap-3 sm:grid-cols-3">
          <StatTile label="ffmpeg" value={settings.ffmpegReady ? 'Ready' : 'Missing'} hint={settings.ffmpegVersion} />
          <StatTile label="Encoder" value={settings.activeEncoder ?? 'unknown'} />
          <StatTile label="Live streams" value={String(sessions?.length ?? 0)} />
        </div>

        <div className="grid gap-4 sm:grid-cols-2">
          <Field
            label="Hardware acceleration"
            htmlFor="hwaccel"
            hint="Only encoders that passed a real trial encode on this machine are listed."
          >
            <select
              id="hwaccel"
              className="select"
              value={settings.hwAccel}
              onChange={(e) => save.mutate({ hwAccel: e.target.value })}
            >
              <option value="auto">Auto (recommended)</option>
              {settings.availableAccels.map((accel) => (
                <option key={accel.kind} value={accel.kind}>
                  {accel.label}
                </option>
              ))}
            </select>
          </Field>

          <Field
            label="Encoder preset"
            htmlFor="preset"
            hint="Slower presets look better at the same bitrate but need more CPU to keep ahead of playback."
          >
            <select
              id="preset"
              className="select"
              value={settings.encoderPreset}
              disabled={settings.presetOptions.length === 0}
              onChange={(e) => save.mutate({ encoderPreset: e.target.value })}
            >
              <option value="">Default for this encoder</option>
              {settings.presetOptions.map((preset) => (
                <option key={preset} value={preset}>
                  {preset}
                </option>
              ))}
            </select>
          </Field>

          <Field
            label="Simultaneous transcodes"
            htmlFor="maxTranscodes"
            hint="Each one is a separate ffmpeg process. Too many will starve the machine."
          >
            <input
              id="maxTranscodes"
              type="number"
              min={1}
              max={16}
              className="input"
              defaultValue={settings.maxTranscodes}
              onBlur={(e) => {
                const value = Number(e.target.value)
                if (value !== settings.maxTranscodes) save.mutate({ maxTranscodes: value })
              }}
            />
          </Field>

          <Field
            label="Audio channels"
            htmlFor="audioChannels"
            hint="Stereo is right for browsers; surround tracks are downmixed to it."
          >
            <select
              id="audioChannels"
              className="select"
              value={settings.audioChannels}
              onChange={(e) => save.mutate({ audioChannels: Number(e.target.value) })}
            >
              <option value={2}>Stereo (2.0)</option>
              <option value={6}>5.1 surround</option>
              <option value={8}>7.1 surround</option>
            </select>
          </Field>
        </div>
      </SectionCard>

      <SectionCard
        title="Bitrate ladder"
        description="Override what each quality rung targets. Leave blank to use the automatic value derived from the source."
        action={
          <button className="btn-sm btn-primary" onClick={saveOverrides} disabled={save.isPending}>
            Save ladder
          </button>
        }
      >
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {RUNGS.map((rung) => (
            <Field key={rung} label={rung} htmlFor={`rung-${rung}`}>
              <div className="relative">
                <input
                  id={`rung-${rung}`}
                  type="number"
                  min={200}
                  step={100}
                  className="input pr-14"
                  placeholder="auto"
                  value={overrideValues[rung] ?? ''}
                  onChange={(e) => setOverride(rung, e.target.value)}
                />
                <span className="pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 text-xs text-ink-400">
                  kbps
                </span>
              </div>
            </Field>
          ))}
        </div>
        <p className="hint">
          A rung is never encoded above the source resolution, and values under 200 kbps are ignored.
        </p>
      </SectionCard>

      <SectionCard title="Behaviour" description="Useful when diagnosing a playback problem.">
        <div className="divide-y divide-ink-700/50">
          <Toggle
            label="Allow direct play"
            description="Send browser-friendly files untouched. Turning this off forces everything through the transcoder, which is the quickest way to reproduce a streaming issue."
            checked={settings.allowDirectPlay}
            onChange={(v) => save.mutate({ allowDirectPlay: v })}
          />
          <Toggle
            label="Burn subtitles into the video by default"
            description="Costs CPU but guarantees subtitles appear on any client. Image-based tracks always burn in regardless."
            checked={settings.burnInSubtitlesByDefault}
            onChange={(v) => save.mutate({ burnInSubtitlesByDefault: v })}
          />
        </div>
      </SectionCard>

      {sessions && sessions.length > 0 && (
        <SectionCard title="Active streams" description="What the server is encoding right now.">
          <div className="space-y-2">
            {sessions.map((session) => (
              <div
                key={session.id}
                className="flex items-center gap-3 rounded-lg border border-ink-700/60 bg-ink-800/50 px-4 py-3"
              >
                {session.hardware ? (
                  <Zap className="h-4 w-4 shrink-0 text-accent" />
                ) : (
                  <Cpu className="h-4 w-4 shrink-0 text-ink-400" />
                )}
                <div className="min-w-0 flex-1">
                  <p className="truncate text-sm font-medium text-ink-100">{session.encoder}</p>
                  <p className="truncate text-xs text-ink-400">
                    {session.ladder.map((r) => r.name).join(', ')} · {Math.round(session.durationSec / 60)} min
                  </p>
                </div>
                <code className="shrink-0 text-[11px] text-ink-500">{session.id.slice(0, 8)}</code>
              </div>
            ))}
          </div>
        </SectionCard>
      )}
    </div>
  )
}
