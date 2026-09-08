// Typed client for the media server API.
//
// The token lives in localStorage rather than a cookie because <video>, <img> and
// <track> elements cannot send an Authorization header; those URLs carry the token as
// an `api_key` query parameter instead, which needs it readable from JavaScript.

const TOKEN_KEY = 'mediaserver.token'

export function getToken(): string | null {
  return localStorage.getItem(TOKEN_KEY)
}

export function setToken(token: string | null) {
  if (token) localStorage.setItem(TOKEN_KEY, token)
  else localStorage.removeItem(TOKEN_KEY)
}

/** ApiError carries the server's structured error so callers can branch on `code`. */
export class ApiError extends Error {
  constructor(
    message: string,
    readonly status: number,
    readonly code?: string,
  ) {
    super(message)
    this.name = 'ApiError'
  }
}

type Method = 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE'

async function request<T>(method: Method, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = {}
  const token = getToken()
  if (token) headers.Authorization = `Bearer ${token}`
  if (body !== undefined) headers['Content-Type'] = 'application/json'

  const res = await fetch(`/api${path}`, {
    method,
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
  })

  if (res.status === 204) return undefined as T

  const text = await res.text()
  let payload: any = null
  if (text) {
    try {
      payload = JSON.parse(text)
    } catch {
      payload = { error: text }
    }
  }

  if (!res.ok) {
    // An expired or revoked token should drop the user back to the login screen
    // rather than leaving every subsequent request failing silently.
    if (res.status === 401 && getToken()) {
      setToken(null)
      window.dispatchEvent(new CustomEvent('mediaserver:signed-out'))
    }
    throw new ApiError(payload?.error ?? `Request failed (${res.status})`, res.status, payload?.code)
  }

  return payload as T
}

export const api = {
  get: <T,>(path: string) => request<T>('GET', path),
  post: <T,>(path: string, body?: unknown) => request<T>('POST', path, body),
  put: <T,>(path: string, body?: unknown) => request<T>('PUT', path, body),
  patch: <T,>(path: string, body?: unknown) => request<T>('PATCH', path, body),
  del: <T,>(path: string) => request<T>('DELETE', path),
}

/** authedUrl appends the token so media elements can fetch protected URLs. */
export function authedUrl(path: string): string {
  const token = getToken()
  if (!token) return path
  const sep = path.includes('?') ? '&' : '?'
  return `${path}${sep}api_key=${encodeURIComponent(token)}`
}

/** imageUrl builds a cached-artwork URL at the requested width. */
export function imageUrl(key: string | undefined, width?: number): string | undefined {
  if (!key) return undefined
  return width ? `/api/images/${key}?w=${width}` : `/api/images/${key}`
}

// ---- shared types ----

export interface ServerInfo {
  name: string
  version: string
  setupRequired: boolean
  hasMetadataKey: boolean
  ffmpegReady: boolean
  ffmpegVersion?: string
  ffmpegHint?: string
}

export interface User {
  id: string
  username: string
  isAdmin: boolean
  createdAt: string
  lastLoginAt?: string
}

export interface AuthResponse {
  token: string
  expiresAt: string
  user: User
}

export type LibraryType = 'movie' | 'show' | 'music' | 'photo'

export interface Library {
  id: string
  name: string
  type: LibraryType
  paths: string[]
  itemCount: number
  createdAt: string
  lastScanAt?: string
  scanning: boolean
}

export interface Page<T> {
  items: T[]
  total: number
  offset: number
  limit: number
}

export interface MovieCard {
  id: string
  title: string
  year?: number
  posterId?: string
  rating?: number
  runtimeMins?: number
  genres?: string[]
  available: boolean
  progress?: number
  watched?: boolean
  /** Video height, used by the quality filter without loading every stream list. */
  height?: number
  durationSec?: number
}

export interface ShowCard {
  id: string
  title: string
  year?: number
  posterId?: string
  rating?: number
  genres?: string[]
  seasonCount: number
  episodeCount: number
  unwatched: number
}

export interface Stream {
  index: number
  kind: 'video' | 'audio' | 'subtitle'
  typeIndex: number
  codec: string
  language?: string
  title?: string
  width?: number
  height?: number
  channels?: number
  channelLayout?: string
}

export interface MediaInfo {
  path: string
  size: number
  container?: string
  durationSec?: number
  bitRate?: number
  streams?: Stream[]
  probed: boolean
  probeError?: string
  available: boolean
}

export interface CastMember {
  name: string
  character?: string
  profileId?: string
  order: number
}

export interface TrackOption {
  index: number
  label: string
  language?: string
  codec?: string
  channels?: number
  default?: boolean
}

export interface MovieDetail {
  id: string
  libraryId: string
  title: string
  year?: number
  overview?: string
  tagline?: string
  genres?: string[]
  runtimeMins?: number
  rating?: number
  releaseDate?: string
  studios?: string[]
  directors?: string[]
  cast?: CastMember[]
  posterId?: string
  backdropId?: string
  tmdbId?: number
  metaStatus: string
  media: MediaInfo
  progress: number
  resumeSec: number
  watched: boolean
  audioTracks: TrackOption[]
  library?: string
}

export interface Episode {
  id: string
  showId: string
  season: number
  episode: number
  title?: string
  overview?: string
  airDate?: string
  stillId?: string
  media: MediaInfo
  progress: number
  resumeSec: number
  watched: boolean
  durationSec: number
  audioTracks?: TrackOption[]
}

export interface Season {
  number: number
  name?: string
  overview?: string
  posterId?: string
  episodes: Episode[]
}

export interface ShowDetail {
  id: string
  title: string
  year?: number
  overview?: string
  genres?: string[]
  rating?: number
  status?: string
  networks?: string[]
  cast?: CastMember[]
  posterId?: string
  backdropId?: string
  metaStatus: string
  seasons: Season[]
  library?: string
  nextUp?: Episode
}

export interface HomeRow {
  kind: 'movie' | 'episode' | 'show'
  id: string
  title: string
  subtitle?: string
  posterId?: string
  backdropId?: string
  year?: number
  progress?: number
  resumeSec?: number
  durationSec?: number
}

export interface HomeData {
  continueWatching: HomeRow[]
  recentMovies: HomeRow[]
  recentShows: HomeRow[]
}

export interface PlaybackInfo {
  mediaId: string
  title: string
  method: 'direct' | 'hls'
  url: string
  sessionId?: string
  durationSec: number
  resumeSec?: number
  container?: string
  videoCodec?: string
  audioCodec?: string
  width?: number
  height?: number
  audioTracks?: TrackOption[]
  reason?: string
}

export interface Rendition {
  name: string
  width: number
  height: number
  videoBitrate: number
  audioBitrate: number
}

export interface SessionInfo {
  id: string
  mediaId: string
  durationSec: number
  segmentSec: number
  ladder: Rendition[]
  audioIndex: number
  encoder: string
  hardware: boolean
  masterUrl: string
}

export interface SubtitleTrack {
  id: string
  label: string
  language?: string
  code?: string
  source: 'sidecar' | 'embedded'
  default?: boolean
  forced?: boolean
  burnInOnly?: boolean
}

export interface ScanProgress {
  libraryId: string
  libraryName: string
  phase: 'walking' | 'probing' | 'metadata' | 'cleaning' | 'done' | 'failed'
  total: number
  done: number
  added: number
  updated: number
  removed: number
  current?: string
  error?: string
  startedAt: string
  finished: boolean
}

export interface ArtistCard {
  id: string
  name: string
  imageId?: string
  albumCount: number
  trackCount: number
}

export interface Track {
  id: string
  albumId: string
  artistId: string
  title: string
  artist?: string
  trackNo?: number
  discNo?: number
  durationSec?: number
  media: MediaInfo
}

export interface Album {
  id: string
  artistId: string
  title: string
  albumArtist?: string
  year?: number
  coverId?: string
  tracks: Track[]
}

export interface Artist {
  id: string
  name: string
  sortName: string
  imageId?: string
  albums: Album[]
}

export interface PhotoAlbumCard {
  id: string
  name: string
  coverId?: string
  photoCount: number
  folderPath?: string
}

export interface Photo {
  id: string
  albumId: string
  filename: string
  width?: number
  height?: number
  takenAt?: string
  camera?: string
  lens?: string
  iso?: number
  aperture?: string
  shutter?: string
  focalLen?: string
  aspectRatio: number
}

export interface UserPrefs {
  id: string
  userId: string

  maxQuality: string
  autoplayNext: boolean
  autoplayCountdown: number
  preferredAudioLang: string
  preferredSubLang: string
  subtitlesDefaultOn: boolean
  seekStepSec: number
  skipIntroEnabled: boolean

  accentColor: string
  posterSize: string
  showTitles: boolean
  reducedMotion: boolean
  heroOnHome: boolean
}

export interface CacheStat {
  path: string
  files: number
  bytes: number
}

export interface LibraryStorage {
  libraryId: string
  libraryName: string
  type: string
  items: number
  bytes: number
}

export interface MaintenanceStats {
  dataDir: string
  databaseBytes: number
  imageCache: CacheStat
  subtitleCache: CacheStat
  transcodeTemp: CacheStat
  libraries: LibraryStorage[]
  activeScans: number
  activeStreams: number
  uptime: string
}

export interface Settings {
  port: number
  hasMetadataKey: boolean
  metadataLang: string
  ffmpegPath: string
  ffprobePath: string
  ffmpegReady: boolean
  ffmpegVersion?: string
  hwAccel: string
  activeEncoder?: string
  availableAccels: { kind: string; label: string }[]
  maxTranscodes: number
  segmentSeconds: number
  sessionIdleSecs: number
  scanOnStart: boolean
  dataDir: string

  encoderPreset: string
  bitrateOverrides: Record<string, number>
  burnInSubtitlesByDefault: boolean
  allowDirectPlay: boolean
  audioChannels: number
  presetOptions: string[]
}
