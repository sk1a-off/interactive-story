import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'

const backend = ['http:', '', '127.0.0.1:8787'].join('/')

console.log('VITE PROXY TARGET =', backend)

export default defineConfig({
  plugins: [react()],
  server: {
    host: '127.0.0.1',
    port: 5173,
    strictPort: true,
    proxy: {
      '/health': {
        target: backend,
        changeOrigin: true,
      },
      '/api': {
        target: backend,
        changeOrigin: true,
      },
      '/internal': {
        target: backend,
        changeOrigin: true,
      },
      '/media': {
        target: backend,
        changeOrigin: true,
      },
    },
  },
  test: {
    environment: 'jsdom',
    setupFiles: './tests/setup.ts',
  },
})
