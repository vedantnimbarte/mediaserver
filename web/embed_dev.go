//go:build !embedui

// Package web carries the built frontend into the binary.
//
// This is the development variant: no assets are embedded, so the server serves only
// the API and the Vite dev server (npm run dev) hosts the UI, proxying /api and /hls
// back to the Go process.
package web

import "io/fs"

// Assets is nil in dev builds; the server detects this and explains how to run the UI.
var Assets fs.FS = nil
