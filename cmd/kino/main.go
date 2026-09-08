// Command mediaserver runs the media server: HTTP API, library scanner, transcoder
// and the embedded web UI, all from a single binary.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"kino/internal/api"
	"kino/internal/config"
	"kino/internal/ffmpeg"
	"kino/internal/images"
	"kino/internal/logbuf"
	"kino/internal/metadata"
	"kino/internal/scanner"
	"kino/internal/store"
	"kino/internal/subtitles"
	"kino/internal/transcode"
	"kino/web"
)

// webFS returns the embedded UI, or nil in dev builds (see web/embed_dev.go).
func webFS() fs.FS { return web.Assets }

// logs retains recent log output for the diagnostics panel.
var logs *logbuf.Buffer

func main() {
	log.SetFlags(log.Ldate | log.Ltime)

	// Mirror everything logged into a ring buffer so the Settings screen can show
	// recent activity without the user needing a terminal or a log file.
	logs = logbuf.New(os.Stderr, logbuf.DefaultCapacity)
	log.SetOutput(logs)

	var (
		dataDir = flag.String("data", "", "data directory (default: ./data next to the executable)")
		port    = flag.Int("port", 0, "override the configured listen port")
		host    = flag.String("host", "", "override the configured listen host")
	)
	flag.Parse()

	if err := run(*dataDir, *host, *port); err != nil {
		log.Fatalf("fatal: %v", err)
	}
}

func run(dataDir, host string, port int) error {
	if dataDir == "" {
		dataDir = config.DefaultDataDir()
	}

	cfg, err := config.Load(dataDir)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	// Command-line flags win over the config file, but are not persisted back.
	if host != "" || port != 0 {
		cfg.Update(func(c *config.Config) {
			if host != "" {
				c.Host = host
			}
			if port != 0 {
				c.Port = port
			}
		})
	}

	db, err := store.OpenDB(dataDir)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			log.Printf("warning: closing database: %v", err)
		}
	}()

	// ffmpeg is required for playback and scanning, but a missing install must not
	// stop the server from booting: the user needs the UI to tell them what is wrong
	// and to point the setting at their own build.
	snapshot := cfg.Snapshot()
	tools, err := ffmpeg.Locate(snapshot.FFmpegPath, snapshot.FFprobePath)
	if err != nil {
		log.Printf("WARNING: %v", err)
		log.Printf("         %s", ffmpeg.InstallHint())
		log.Printf("         The server will start, but scanning and playback will not work.")
	} else {
		log.Printf("ffmpeg %s at %s", tools.Version, tools.FFmpeg)
	}

	imageCache := images.New(cfg.ImageDir())
	enricher := metadata.NewEnricher(
		metadata.NewClient(snapshot.TMDbAPIKey, snapshot.MetadataLang),
		imageCache,
	)
	if enricher.Enabled() {
		log.Printf("metadata: TMDb lookups enabled (%s)", snapshot.MetadataLang)
	} else {
		log.Printf("metadata: no TMDb API key set; titles come from filenames only")
	}

	scan := scanner.New(db, tools, enricher, imageCache)
	defer scan.CancelAll()

	transcoder := transcode.NewManager(transcode.Options{
		Tools:            tools,
		RootDir:          cfg.TranscodeDir(),
		SegmentSeconds:   snapshot.SegmentSeconds,
		IdleSeconds:      snapshot.SessionIdleSecs,
		MaxConcurrent:    snapshot.MaxTranscodes,
		HWAccel:          snapshot.HWAccel,
		EncoderPreset:    snapshot.EncoderPreset,
		BitrateOverrides: snapshot.BitrateOverrides,
		AudioChannels:    snapshot.AudioChannels,
	})
	// Close before the database so that every encoder is dead and every scratch
	// directory removed before the process exits.
	defer transcoder.Close()

	subs := subtitles.NewService(tools, cfg.SubtitleDir())

	srv := api.New(api.Options{
		Config:     cfg,
		DB:         db,
		Web:        webFS(),
		Scanner:    scan,
		Transcoder: transcoder,
		Subtitles:  subs,
		Images:     imageCache,
		Metadata:   enricher,
		Logs:       logs,
		Tools:      tools,
	})

	if snapshot.ScanOnStart {
		go func() {
			for _, lib := range db.Libraries.All() {
				if err := scan.Scan(context.Background(), lib.ID); err != nil {
					log.Printf("startup scan of %s: %v", lib.Name, err)
				}
			}
		}()
	}

	httpSrv := &http.Server{
		Addr:    cfg.Addr(),
		Handler: srv,
		// No WriteTimeout: streaming a two-hour movie or holding an SSE connection
		// open legitimately outlives any fixed write deadline.
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	ln, err := net.Listen("tcp", httpSrv.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", httpSrv.Addr, err)
	}

	log.Printf("data directory: %s", dataDir)
	log.Printf("listening on http://%s", httpSrv.Addr)
	for _, url := range lanURLs(cfg.Port) {
		log.Printf("  reachable at %s", url)
	}
	if db.Users.Len() == 0 {
		log.Printf("no users yet: open the UI to create the admin account")
	}

	errCh := make(chan error, 1)
	go func() {
		if err := httpSrv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	// Wait for either a fatal serve error or a shutdown signal.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-errCh:
		return fmt.Errorf("http server: %w", err)
	case sig := <-sigCh:
		log.Printf("received %s, shutting down", sig)
	}

	// Give in-flight requests a moment, then force the issue. The deferred db.Close
	// performs the final flush after this returns.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(ctx); err != nil {
		log.Printf("warning: graceful shutdown timed out: %v", err)
	}
	return nil
}

// lanURLs lists the addresses other devices on the network can use, which saves the
// user from hunting for their IP when they want to watch on a phone.
func lanURLs(port int) []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []string
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipNet.IP.To4()
			if ip == nil || !ip.IsPrivate() {
				continue
			}
			out = append(out, fmt.Sprintf("http://%s:%d", ip, port))
		}
	}
	return out
}
