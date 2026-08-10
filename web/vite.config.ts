import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// The Go backend embeds the built frontend from backend/web/dist.
// Build output is directed there via `vite build --outDir`.
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      // Proxy API calls to the Go backend during development.
      '/api': 'http://127.0.0.1:8080',
    },
  },
  build: {
    outDir: '../backend/web/dist',
    emptyOutDir: true,
  },
})