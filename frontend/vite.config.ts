import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

// API_TARGET points the dev proxy at another backend, e.g. API_TARGET=https://similar.earth
const apiTarget = process.env.API_TARGET || 'http://localhost:8080'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    port: 3000,
    proxy: {
      '/api': {
        target: apiTarget,
        changeOrigin: true,
      },
      '/tiles': {
        target: apiTarget,
        changeOrigin: true,
      },
    },
  },
})
