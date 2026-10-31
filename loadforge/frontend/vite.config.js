import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  build: { chunkSizeWarningLimit: 1200 },
  server: {
    port: 5173,
    proxy: { '/api': { target: process.env.API_URL || 'http://localhost:8080', changeOrigin: true } },
  },
})
