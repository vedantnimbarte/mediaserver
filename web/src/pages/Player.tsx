import { useCallback, useEffect, useRef, useState } from 'react'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import Hls from 'hls.js'
import {
  ArrowLeft,
  Captions,
  Gauge,
  Loader2,
  Maximize,
  Minimize,
  Pause,
  Play,
  SkipForward,
  Volume2,
  VolumeX,
} from 'lucide-react'

import {
  api,
  authedUrl,
  type PlaybackInfo,
  type SessionInfo,
  type SubtitleTrack,
  type TrackOption,
} from '../lib/api'
import { ErrorBox, formatTime } from '../components/ui'
import { usePrefs } from '../lib/prefs'

interface StartResponse {
  playback: PlaybackInfo
  session?: SessionInfo
}

interface NextEpisode {
  id: string
  season: number
  episode: number
  title?: string
}

/** How often progress is reported to the server while playing. */
const PROGRESS_INTERVAL_MS = 10_000

export default function Player() {
  const { mediaId = '' } = useParams()
  const [params] = useSearchParams()
  const navigate = useNavigate()
  const { prefs } = usePrefs()

  const videoRef = useRef<HTMLVideoElement>(null)
  const hlsRef = useRef<Hls | null>(null)
  const containerRef = useRef<HTMLDivElement>(null)
  const hideControlsTimer = useRef<number | null>(null)

  const [info, setInfo] = useState<PlaybackInfo | null>(null)
  const [session, setSession] = useState<SessionInfo | null>(null)
  const [subtitles, setSubtitles] = useState<SubtitleTrack[]>([])
  const [nextEpisode, setNextEpisode] = useState<NextEpisode | null>(null)
  const [error, setError] = useState<string | null>(null)

  const [playing, setPlaying] = useState(false)
  const [buffering, setBuffering] = useState(true)
  const [current, setCurrent] = useState(0)
  const [duration, setDuration] = useState(0)
  const [volume, setVolume] = useState(1)
  const [muted, setMuted] = useState(false)
  const [fullscreen, setFullscreen] = useState(false)
  const [controlsVisible, setControlsVisible] = useState(true)

  const [levels, setLevels] = useState<{ index: number; label: string }[]>([])
  const [currentLevel, setCurrentLevel] = useState(-1)
  const [activeSubtitle, setActiveSubtitle] = useState<string>('off')
  const [audioTracks, setAudioTracks] = useState<TrackOption[]>([])
  const [audioIndex, setAudioIndex] = useState<number>(-1)
  const [menu, setMenu] = useState<'quality' | 'subs' | 'audio' | null>(null)
  const [countdown, setCountdown] = useState<number | null>(null)

  const startFrom = params.get('from')

  // ---- start playback ----

  const start = useCallback(
    async (opts: { audioIndex?: number; burnSubtitleIndex?: number; seekTo?: number } = {}) => {
      setError(null)
      setBuffering(true)

      try {
        const resp = await api.post<StartResponse | PlaybackInfo>('/playback/start', {
          mediaId,
          audioIndex: opts.audioIndex,
          burnSubtitleIndex: opts.burnSubtitleIndex,
        })

        // Direct play returns the playback info alone; a transcode wraps it with the
        // session that owns the HLS stream.
        const playback = 'playback' in resp ? resp.playback : (resp as PlaybackInfo)
        const sess = 'session' in resp ? resp.session : undefined

        setInfo(playback)
        setSession(sess ?? null)
        setAudioTracks(playback.audioTracks ?? [])
        if (opts.audioIndex === undefined && sess) setAudioIndex(sess.audioIndex)

        const resumeAt =
          opts.seekTo ?? (startFrom !== null ? Number(startFrom) : playback.resumeSec ?? 0)
        attach(playback, resumeAt)
      } catch (err: any) {
        setError(err?.message ?? 'Playback could not be started.')
        setBuffering(false)
      }
    },
    // attach is stable enough for this: it only reads refs and setState.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [mediaId, startFrom],
  )

  // attach wires the media source into the <video> element.
  const attach = useCallback((playback: PlaybackInfo, seekTo: number) => {
    const video = videoRef.current
    if (!video) return

    // Tear down any previous HLS instance, otherwise its workers keep fetching
    // segments for a stream nobody is watching.
    if (hlsRef.current) {
      hlsRef.current.destroy()
      hlsRef.current = null
    }

    if (playback.method === 'direct') {
      video.src = authedUrl(playback.url)
      if (seekTo > 0) video.currentTime = seekTo
      video.play().catch(() => setPlaying(false))
      return
    }

    const src = authedUrl(playback.url)

    if (Hls.isSupported()) {
      const hls = new Hls({
        // The server encodes on demand, so a segment request can legitimately take a
        // few seconds. The defaults give up far too eagerly and surface as an error.
        manifestLoadingTimeOut: 30_000,
        fragLoadingTimeOut: 60_000,
        fragLoadingMaxRetry: 4,
        levelLoadingTimeOut: 30_000,
        startPosition: seekTo > 0 ? seekTo : -1,
        maxBufferLength: 30,
      })

      hls.on(Hls.Events.MANIFEST_PARSED, (_evt, data) => {
        setLevels(
          data.levels.map((level, index) => ({
            index,
            label: level.height ? `${level.height}p` : `${Math.round(level.bitrate / 1000)}k`,
          })),
        )

        // Honour the viewer's quality cap by forbidding anything taller than it,
        // rather than just preselecting a level — otherwise ABR would climb straight
        // back above the cap on a fast connection.
        const cap = Number(prefs.maxQuality.replace('p', ''))
        if (Number.isFinite(cap) && cap > 0) {
          const allowed = data.levels
            .map((level, index) => ({ index, height: level.height ?? 0 }))
            .filter((l) => l.height <= cap)
          if (allowed.length > 0) {
            hls.autoLevelCapping = allowed[allowed.length - 1].index
          }
        }

        video.play().catch(() => setPlaying(false))
      })

      hls.on(Hls.Events.LEVEL_SWITCHED, (_evt, data) => setCurrentLevel(data.level))

      hls.on(Hls.Events.ERROR, (_evt, data) => {
        if (!data.fatal) return
        switch (data.type) {
          case Hls.ErrorTypes.NETWORK_ERROR:
            // A segment that timed out is usually the encoder still catching up.
            hls.startLoad()
            break
          case Hls.ErrorTypes.MEDIA_ERROR:
            hls.recoverMediaError()
            break
          default:
            setError('Playback failed. The stream may have expired; go back and press play again.')
            hls.destroy()
        }
      })

      hls.loadSource(src)
      hls.attachMedia(video)
      hlsRef.current = hls
    } else if (video.canPlayType('application/vnd.apple.mpegurl')) {
      // Safari plays HLS natively and does not need hls.js.
      video.src = src
      if (seekTo > 0) {
        video.addEventListener('loadedmetadata', () => (video.currentTime = seekTo), { once: true })
      }
      video.play().catch(() => setPlaying(false))
    } else {
      setError('This browser cannot play the required video format.')
    }
  }, [])

  useEffect(() => {
    start()
    return () => {
      if (hlsRef.current) {
        hlsRef.current.destroy()
        hlsRef.current = null
      }
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [mediaId])

  // Load the subtitle list and, for episodes, what comes next.
  useEffect(() => {
    api
      .get<SubtitleTrack[]>(`/media/${mediaId}/subtitles`)
      .then((tracks) => {
        setSubtitles(tracks)

        // Auto-enable subtitles when the viewer has asked for that, preferring their
        // chosen language and falling back to a forced track, which is what people
        // usually want for a foreign-language film.
        if (!prefs.subtitlesDefaultOn) return
        const usable = tracks.filter((t) => !t.burnInOnly)
        const match =
          usable.find((t) => prefs.preferredSubLang && t.code?.startsWith(prefs.preferredSubLang.slice(0, 2))) ??
          usable.find((t) => t.forced) ??
          usable[0]
        if (match) {
          // Defer until the <track> elements exist in the DOM.
          window.setTimeout(() => selectSubtitle(match.id), 300)
        }
      })
      .catch(() => setSubtitles([]))

    api
      .get<{ nextEpisode?: NextEpisode }>(`/episodes/${mediaId}`)
      .then((data) => setNextEpisode(data.nextEpisode ?? null))
      .catch(() => setNextEpisode(null))
  }, [mediaId])

  // Stop the transcode session when leaving, so the encoder does not keep running
  // until the idle collector notices.
  useEffect(() => {
    const sessionId = session?.id
    if (!sessionId) return
    return () => {
      api.post(`/playback/${sessionId}/stop`).catch(() => {})
    }
  }, [session?.id])

  // ---- progress reporting ----

  const report = useCallback(
    (finished = false) => {
      const video = videoRef.current
      if (!video || !video.duration) return
      api
        .post(`/playstate/${mediaId}`, {
          positionSec: video.currentTime,
          durationSec: video.duration,
          finished,
        })
        .catch(() => {})
    },
    [mediaId],
  )

  useEffect(() => {
    const timer = window.setInterval(() => {
      if (videoRef.current && !videoRef.current.paused) report()
    }, PROGRESS_INTERVAL_MS)

    // A closed tab never fires React cleanup, so the final position has to be sent
    // from a page-lifecycle event or the resume point is silently lost.
    const onHide = () => report()
    window.addEventListener('pagehide', onHide)
    document.addEventListener('visibilitychange', onHide)

    return () => {
      window.clearInterval(timer)
      window.removeEventListener('pagehide', onHide)
      document.removeEventListener('visibilitychange', onHide)
      report()
    }
  }, [report])

  // ---- controls ----

  const togglePlay = useCallback(() => {
    const video = videoRef.current
    if (!video) return
    if (video.paused) video.play().catch(() => {})
    else video.pause()
  }, [])

  const seekBy = useCallback((delta: number) => {
    const video = videoRef.current
    if (!video) return
    video.currentTime = Math.max(0, Math.min(video.duration || 0, video.currentTime + delta))
  }, [])

  const toggleFullscreen = useCallback(async () => {
    if (!document.fullscreenElement) {
      await containerRef.current?.requestFullscreen().catch(() => {})
    } else {
      await document.exitFullscreen().catch(() => {})
    }
  }, [])

  useEffect(() => {
    const onChange = () => setFullscreen(Boolean(document.fullscreenElement))
    document.addEventListener('fullscreenchange', onChange)
    return () => document.removeEventListener('fullscreenchange', onChange)
  }, [])

  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.target instanceof HTMLInputElement || e.target instanceof HTMLSelectElement) return

      // Jump to a percentage of the runtime, the way every video site does.
      if (e.key >= '0' && e.key <= '9') {
        const video = videoRef.current
        if (video?.duration) {
          video.currentTime = (Number(e.key) / 10) * video.duration
          showControls()
        }
        return
      }

      const step = prefs.seekStepSec

      switch (e.key) {
        case ' ':
        case 'k':
          e.preventDefault()
          togglePlay()
          break
        case 'ArrowLeft':
          seekBy(-step)
          break
        case 'ArrowRight':
          seekBy(step)
          break
        case 'ArrowUp':
          e.preventDefault()
          setVolume((v) => Math.min(1, v + 0.05))
          setMuted(false)
          break
        case 'ArrowDown':
          e.preventDefault()
          setVolume((v) => Math.max(0, v - 0.05))
          break
        case 'j':
          seekBy(-30)
          break
        case 'l':
          seekBy(30)
          break
        case 'f':
          toggleFullscreen()
          break
        case 'm':
          setMuted((m) => !m)
          break
        case 'c':
          // Cycle between off and the first usable track.
          setActiveSubtitle((current) => {
            const usable = subtitles.filter((s) => !s.burnInOnly)
            const nextId = current === 'off' ? (usable[0]?.id ?? 'off') : 'off'
            applySubtitle(nextId)
            return nextId
          })
          break
        case 'n':
          if (nextEpisode) navigate(`/play/${nextEpisode.id}`)
          break
        case 'Escape':
          if (!document.fullscreenElement) navigate(-1)
          break
      }
      showControls()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [togglePlay, seekBy, toggleFullscreen, prefs.seekStepSec, subtitles, nextEpisode])

  const showControls = useCallback(() => {
    setControlsVisible(true)
    if (hideControlsTimer.current) window.clearTimeout(hideControlsTimer.current)
    hideControlsTimer.current = window.setTimeout(() => {
      if (videoRef.current && !videoRef.current.paused) setControlsVisible(false)
    }, 3000)
  }, [])

  useEffect(() => {
    const video = videoRef.current
    if (!video) return
    video.volume = volume
    video.muted = muted
  }, [volume, muted])

  // ---- next-episode autoplay ----

  useEffect(() => {
    if (countdown === null) return
    if (countdown <= 0) {
      if (nextEpisode) navigate(`/play/${nextEpisode.id}`)
      return
    }
    const timer = window.setTimeout(() => setCountdown((c) => (c ?? 1) - 1), 1000)
    return () => window.clearTimeout(timer)
  }, [countdown, nextEpisode, navigate])

  function onEnded() {
    report(true)
    setPlaying(false)
    // Only queue the next episode when the viewer has asked for autoplay.
    if (nextEpisode && prefs.autoplayNext) setCountdown(prefs.autoplayCountdown)
  }

  // ---- track switching ----

  async function selectAudio(index: number) {
    setMenu(null)
    if (index === audioIndex) return
    const at = videoRef.current?.currentTime ?? 0
    setAudioIndex(index)
    // Changing the audio track requires a new transcode, so the session restarts at
    // the current position rather than from the beginning.
    await start({ audioIndex: index, seekTo: at })
  }

  /** applySubtitle flips the <track> modes; it does not touch React state. */
  function applySubtitle(id: string) {
    const video = videoRef.current
    if (!video) return
    for (let i = 0; i < video.textTracks.length; i++) {
      const track = video.textTracks[i]
      track.mode = track.id === id ? 'showing' : 'disabled'
    }
  }

  function selectSubtitle(id: string) {
    setMenu(null)
    setActiveSubtitle(id)
    applySubtitle(id)
  }

  function selectLevel(index: number) {
    setMenu(null)
    if (hlsRef.current) hlsRef.current.currentLevel = index
    setCurrentLevel(index)
  }

  if (error) {
    return (
      <div className="flex h-full items-center justify-center bg-black p-6">
        <ErrorBox
          title="Cannot play this"
          message={error}
          action={
            <button className="btn-ghost" onClick={() => navigate(-1)}>
              Go back
            </button>
          }
        />
      </div>
    )
  }

  const textSubtitles = subtitles.filter((s) => !s.burnInOnly)

  return (
    <div
      ref={containerRef}
      className="relative h-full w-full overflow-hidden bg-black"
      onMouseMove={showControls}
      onClick={showControls}
    >
      <video
        ref={videoRef}
        className="h-full w-full"
        playsInline
        crossOrigin="anonymous"
        onPlay={() => setPlaying(true)}
        onPause={() => {
          setPlaying(false)
          setControlsVisible(true)
          report()
        }}
        onTimeUpdate={(e) => setCurrent(e.currentTarget.currentTime)}
        onDurationChange={(e) => setDuration(e.currentTarget.duration)}
        onWaiting={() => setBuffering(true)}
        onPlaying={() => setBuffering(false)}
        onCanPlay={() => setBuffering(false)}
        onEnded={onEnded}
        onDoubleClick={toggleFullscreen}
      >
        {textSubtitles.map((track) => (
          <track
            key={track.id}
            id={track.id}
            kind="subtitles"
            label={track.label}
            srcLang={track.code}
            src={authedUrl(`/api/media/${mediaId}/subtitles/${track.id}.vtt`)}
          />
        ))}
      </video>

      {buffering && (
        <div className="pointer-events-none absolute inset-0 flex items-center justify-center">
          <Loader2 className="h-12 w-12 animate-spin text-white/80" />
        </div>
      )}

      {/* Next-episode card */}
      {countdown !== null && nextEpisode && (
        <div className="absolute bottom-24 right-6 w-72 rounded-xl border border-ink-700 bg-ink-900/95 p-4 shadow-2xl">
          <p className="text-xs uppercase tracking-wide text-ink-400">Up next</p>
          <p className="mt-1 text-sm font-medium text-white">
            S{String(nextEpisode.season).padStart(2, '0')}E
            {String(nextEpisode.episode).padStart(2, '0')}
            {nextEpisode.title ? ` · ${nextEpisode.title}` : ''}
          </p>
          <p className="mt-1 text-xs text-ink-400">Playing in {countdown}s</p>
          <div className="mt-3 flex gap-2">
            <button className="btn-primary flex-1" onClick={() => navigate(`/play/${nextEpisode.id}`)}>
              <SkipForward className="h-4 w-4" />
              Play now
            </button>
            <button className="btn-ghost" onClick={() => setCountdown(null)}>
              Cancel
            </button>
          </div>
        </div>
      )}

      {/* Controls overlay */}
      <div
        className={`absolute inset-x-0 bottom-0 bg-gradient-to-t from-black/90 to-transparent px-4 pb-4 pt-16 transition-opacity ${
          controlsVisible ? 'opacity-100' : 'pointer-events-none opacity-0'
        }`}
      >
        <input
          type="range"
          min={0}
          max={duration || info?.durationSec || 0}
          step={0.1}
          value={current}
          onChange={(e) => {
            const video = videoRef.current
            if (video) video.currentTime = Number(e.target.value)
          }}
          className="mb-3 h-1 w-full cursor-pointer appearance-none rounded bg-white/25 accent-accent"
          aria-label="Seek"
        />

        <div className="flex items-center gap-3 text-white">
          <button onClick={togglePlay} className="rounded p-1 hover:bg-white/10" aria-label={playing ? 'Pause' : 'Play'}>
            {playing ? <Pause className="h-6 w-6" /> : <Play className="h-6 w-6 fill-current" />}
          </button>

          <div className="flex items-center gap-2">
            <button onClick={() => setMuted((m) => !m)} className="rounded p-1 hover:bg-white/10" aria-label="Mute">
              {muted || volume === 0 ? <VolumeX className="h-5 w-5" /> : <Volume2 className="h-5 w-5" />}
            </button>
            <input
              type="range"
              min={0}
              max={1}
              step={0.05}
              value={muted ? 0 : volume}
              onChange={(e) => {
                setVolume(Number(e.target.value))
                setMuted(false)
              }}
              className="hidden h-1 w-20 cursor-pointer appearance-none rounded bg-white/25 accent-accent sm:block"
              aria-label="Volume"
            />
          </div>

          <span className="text-xs tabular-nums text-white/80">
            {formatTime(current)} / {formatTime(duration || info?.durationSec || 0)}
          </span>

          <div className="ml-auto flex items-center gap-1">
            {audioTracks.length > 1 && (
              <MenuButton
                label="Audio"
                open={menu === 'audio'}
                onToggle={() => setMenu(menu === 'audio' ? null : 'audio')}
                icon={<Volume2 className="h-5 w-5" />}
              >
                {audioTracks.map((track) => (
                  <MenuItem
                    key={track.index}
                    active={track.index === audioIndex}
                    onClick={() => selectAudio(track.index)}
                  >
                    {track.label}
                  </MenuItem>
                ))}
              </MenuButton>
            )}

            {textSubtitles.length > 0 && (
              <MenuButton
                label="Subtitles"
                open={menu === 'subs'}
                onToggle={() => setMenu(menu === 'subs' ? null : 'subs')}
                icon={<Captions className="h-5 w-5" />}
              >
                <MenuItem active={activeSubtitle === 'off'} onClick={() => selectSubtitle('off')}>
                  Off
                </MenuItem>
                {textSubtitles.map((track) => (
                  <MenuItem
                    key={track.id}
                    active={activeSubtitle === track.id}
                    onClick={() => selectSubtitle(track.id)}
                  >
                    {track.label}
                  </MenuItem>
                ))}
              </MenuButton>
            )}

            {levels.length > 1 && (
              <MenuButton
                label="Quality"
                open={menu === 'quality'}
                onToggle={() => setMenu(menu === 'quality' ? null : 'quality')}
                icon={<Gauge className="h-5 w-5" />}
              >
                <MenuItem active={currentLevel === -1} onClick={() => selectLevel(-1)}>
                  Auto
                </MenuItem>
                {levels.map((level) => (
                  <MenuItem
                    key={level.index}
                    active={currentLevel === level.index}
                    onClick={() => selectLevel(level.index)}
                  >
                    {level.label}
                  </MenuItem>
                ))}
              </MenuButton>
            )}

            <button onClick={toggleFullscreen} className="rounded p-1 hover:bg-white/10" aria-label="Fullscreen">
              {fullscreen ? <Minimize className="h-5 w-5" /> : <Maximize className="h-5 w-5" />}
            </button>
          </div>
        </div>
      </div>

      {/* Title bar */}
      <div
        className={`absolute inset-x-0 top-0 flex items-center gap-3 bg-gradient-to-b from-black/80 to-transparent px-4 py-4 transition-opacity ${
          controlsVisible ? 'opacity-100' : 'pointer-events-none opacity-0'
        }`}
      >
        <button onClick={() => navigate(-1)} className="rounded p-1 text-white hover:bg-white/10" aria-label="Back">
          <ArrowLeft className="h-6 w-6" />
        </button>
        <div className="min-w-0">
          <p className="truncate text-sm font-medium text-white">{info?.title}</p>
          {info && (
            <p className="truncate text-xs text-white/60">
              {info.method === 'direct'
                ? 'Direct play'
                : `Transcoding${session ? ` · ${session.encoder}` : ''}${info.reason ? ` · ${info.reason}` : ''}`}
            </p>
          )}
        </div>
      </div>
    </div>
  )
}

function MenuButton({
  label,
  icon,
  open,
  onToggle,
  children,
}: {
  label: string
  icon: React.ReactNode
  open: boolean
  onToggle: () => void
  children: React.ReactNode
}) {
  return (
    <div className="relative">
      <button onClick={onToggle} className="rounded p-1 hover:bg-white/10" aria-label={label} title={label}>
        {icon}
      </button>
      {open && (
        <div className="absolute bottom-full right-0 mb-2 max-h-64 min-w-44 overflow-y-auto rounded-lg border border-ink-700 bg-ink-900/98 py-1 shadow-2xl">
          <p className="px-3 py-1 text-xs font-medium uppercase tracking-wide text-ink-500">{label}</p>
          {children}
        </div>
      )}
    </div>
  )
}

function MenuItem({
  active,
  onClick,
  children,
}: {
  active: boolean
  onClick: () => void
  children: React.ReactNode
}) {
  return (
    <button
      onClick={onClick}
      className={`block w-full px-3 py-1.5 text-left text-sm transition-colors hover:bg-ink-800 ${
        active ? 'text-accent' : 'text-ink-200'
      }`}
    >
      {children}
    </button>
  )
}
