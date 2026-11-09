import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'
import { resolveBackendTarget } from './src/lib/viteBackendTarget'

const backend = resolveBackendTarget(process.env.VITE_DEV_BACKEND_TARGET)

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
