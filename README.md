# KINO

A self-hosted media server. Point it at folders of movies, TV, music and photos, and
stream them to any browser on your network — transcoding on the fly when the source
format is something a browser cannot play.

Go backend, React frontend, JSON files on disk for storage. Ships as a single
executable with the web UI compiled in.

## Features

- **Four library types** — movies, TV shows, music, photos.
- **Direct play** for browser-friendly files (mp4/H.264/AAC), served with HTTP range
  requests at effectively zero CPU cost.
- **Adaptive-bitrate HLS** for everything else (mkv, HEVC, AC3, DTS…). The ladder is
  derived from the source and never upscales.
- **Hardware acceleration** — NVENC, Quick Sync, AMF and VideoToolbox are probed with
  a real trial encode at startup, with automatic fallback to libx264.
- **Real seeking** — jumping to the middle of a two-hour film restarts the encoder at
  that point rather than transcoding everything before it.
- **Subtitles** — sidecar `.srt`/`.ass`/`.vtt` files and embedded text tracks, all
  converted to WebVTT. Image-based tracks (PGS, VOBSUB) are flagged for burn-in.
- **Audio track switching** with automatic stereo AAC downmix.
- **Multi-user** accounts with per-user watch history, resume points, preferences,
  Continue Watching and next-episode autoplay.
- **Metadata** from TMDb, with artwork cached locally so the library works offline
  after the first scan.
- **Incremental scans** — unchanged files skip ffprobe entirely.

## Interface

The UI is modelled on Netflix: a near-black shell so artwork is the brightest thing on
screen, a full-bleed billboard on the home screen, horizontal carousels with arrow
scrubbers, and cards that expand on hover to reveal quick actions.

- **Toasts and dialogs** instead of browser alerts.
- **Skeleton loaders** shaped like the content, so pages do not jump as data arrives.
- **Right-click quick actions** on any poster — play, mark watched, go to show.
- **Filter bar** for genre, year, watched state and quality, with removable chips.
- **Keyboard shortcuts** throughout; press <kbd>?</kbd> for the full list.

## Requirements

- **ffmpeg and ffprobe** (required for scanning and playback)
- Go 1.23+ and Node 18+ (only to build from source)

```powershell
winget install Gyan.FFmpeg        # Windows
brew install ffmpeg               # macOS
sudo apt install ffmpeg           # Debian/Ubuntu
```

If ffmpeg is not on your `PATH`, KINO looks in the usual portable-install locations,
and you can set an explicit folder in Settings.

## Build and run

```bash
# Build the UI, then compile it into the binary
cd web && npm install && npm run build && cd ..
go build -tags embedui -o kino ./cmd/kino

./kino
```

Then open <http://localhost:8096> and create the administrator account.

Flags: `-port`, `-host`, `-data` (defaults to a `data` folder beside the executable).

### Development

The `embedui` build tag is what compiles the UI in. Without it the Go server runs
API-only, which is what you want while developing:

```bash
go run ./cmd/kino                 # API on :8096
cd web && npm run dev             # UI on :5173, proxying to :8096
```

## Setup

1. Sign in and open **Settings → Libraries → Add library**.
2. Pick the type, give it a name, and enter the full path to the folder *on the machine
   running the server*. It scans immediately.
3. Optionally add a free [TMDb](https://www.themoviedb.org/settings/api) API key under
   **Settings → Server** for posters, descriptions and cast. Without one, titles come
   from filenames and ffprobe.

### Folder layout

The scanner handles the common conventions:

```
Movies/
  The Matrix (1999)/The Matrix (1999).mkv
  Inception.2010.1080p.BluRay.x264-GROUP.mkv

TV/
  Breaking Bad/Season 01/Breaking Bad - S01E01 - Pilot.mkv
  Firefly/Firefly.S01E01.Serenity.mkv

Music/
  Aphex Twin/Selected Ambient Works/01 Xtal.mp3     (tags win; paths are the fallback)

Photos/
  Holiday 2023/...                                   (each folder is an album)
```

Samples, trailers and extras are skipped.

## Settings

| Panel | What it covers |
|---|---|
| **Libraries** | Add, scan and remove libraries, with live scan progress |
| **Playback** | Quality cap, autoplay and countdown, preferred audio and subtitle language, seek step — per user |
| **Appearance** | Accent colour, poster density, titles, billboard, reduced motion — per user |
| **Users** | Accounts and admin roles |
| **Transcoding** | Hardware acceleration, encoder preset, bitrate ladder overrides, audio channels, direct-play toggle, live streams |
| **Maintenance** | Storage by library, cache sizes and clearing, server log, backup and restore |
| **Server** | TMDb key and language, scan on startup, port, ffmpeg path |

Playback and appearance settings are per-user; everything else is server-wide and
admin-only.

## How it works

**Storage.** Every collection lives in memory and is mirrored to a JSON file in the
data directory. Writes are debounced and atomic (temp file → fsync → rename), and the
previous version is rotated to `.bak`, so a crash mid-write cannot corrupt a library.
A file that fails to parse is quarantined and recovered from the backup. This is sized
for libraries in the low thousands of items.

**Transcoding.** Playlists are generated by Go from the known duration, listing every
segment up front, so a player can seek anywhere immediately. ffmpeg is only started
when a segment is actually requested, and only for the rendition being watched. A
request far ahead of the encoder is treated as a seek: the worker is killed and
relaunched at that point. Sessions are reaped after 60 idle seconds, and on Windows
every encoder is enrolled in a job object so that even an abnormal server exit cannot
leave orphaned ffmpeg processes behind.

**Auth.** Stateless JWTs carrying a per-user token version, so bumping that version
signs a user out everywhere without the server keeping a session table.

## Testing

```bash
go test ./...           # unit and integration tests
cd web && npm run build # type-checks the frontend
```

The integration tests synthesize real media with ffmpeg and run it through the actual
scanner and transcoder, so they cover encoding, seeking and cleanup rather than mocks.
They skip automatically if ffmpeg is not installed.

## Layout

```
cmd/kino/            entry point
internal/
  api/               HTTP handlers and routing
  auth/              password hashing, JWTs
  config/            config.json
  ffmpeg/            binary discovery, ffprobe, encoder detection
  images/            artwork cache and resizing
  logbuf/            in-memory log ring for the diagnostics panel
  metadata/          TMDb client and matching
  models/            persisted entities
  scanner/           filesystem walking, filename parsing, library building
  store/             JSON-backed collections
  subtitles/         discovery, extraction, WebVTT conversion
  transcode/         HLS sessions, ABR ladder, segment workers
web/                 React frontend
```
