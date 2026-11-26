import stylex from '@stylexjs/unplugin'
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vitest/config'
import { version } from './package.json'

export default defineConfig({
  define: { __APP_VERSION__: JSON.stringify(version) },
  resolve: {
    tsconfigPaths: true,
  },
  plugins: [
    stylex.vite({ runtimeInjection: false, useCSSLayers: true }),
    react(),
  ],
  server: {
    proxy: { '/api/v1': process.env['VEKTOR_DEV_API'] || 'http://127.0.0.1:8080' },
  },
  test: { include: ['src/**/*.test.{ts,tsx}'] },
})
