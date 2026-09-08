import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// In development the Go server runs on :8096 and Vite serves the UI on :5173,
// proxying the API and HLS routes across so that cookies, tokens and relative
// segment URLs all behave exactly as they will in the built single-binary version.
const backend = process.env.MEDIASERVER_URL ?? 'http://127.0.0.1:8096'

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/api': { target: backend, changeOrigin: true },
      '/hls': { target: backend, changeOrigin: true },
    },
  },
  build: {
    outDir: 'dist',
    // The Go binary embeds this directory; keeping the manifest out avoids shipping
    // a file the server would have to special-case.
    manifest: false,
    chunkSizeWarningLimit: 900,
  },
})
