import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  base: process.env.KALOQ_WEB_BASE || '/',
  server: {
    port: 5174,
    proxy: { '/api/v1': { target: process.env.KALOQ_API_TARGET || 'http://127.0.0.1:4201', changeOrigin: false } },
  },
})
