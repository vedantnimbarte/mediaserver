//go:build embedui

// Package web carries the built frontend into the binary.
//
// The embed is behind a build tag so that `go build ./...` works before anyone has
// run `npm run build`. Release builds use:
//
//	cd web && npm run build && cd .. && go build -tags embedui ./cmd/mediaserver
package web

import (
	"embed"
	"io/fs"
	"log"
)

//go:embed all:dist
var dist embed.FS

// Assets is the built SPA rooted at dist/, or nil when the UI was not embedded.
var Assets fs.FS = mustSub()

func mustSub() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		log.Fatalf("web: embedded dist is unusable: %v", err)
	}
	return sub
}
