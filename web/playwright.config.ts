import { defineConfig, devices } from '@playwright/test'

const baseURL = process.env['VEKTOR_E2E_URL'] || `http://127.0.0.1:${process.env['VEKTOR_E2E_PORT'] || '18437'}`
const executablePath = process.env['VEKTOR_CHROMIUM_PATH']

export default defineConfig({
  testDir: './e2e',
  workers: 1, // The live suite shares an isolated daemon with the real login rate limiter.
  timeout: 65_000,
  reporter: 'list',
  use: {
    ...devices['Desktop Chrome'],
    viewport: { width: 1440, height: 960 },
    baseURL,
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    launchOptions: executablePath ? { executablePath } : undefined,
  },
  ...(process.env['VEKTOR_E2E_URL'] ? {} : {
    webServer: {
      command: 'bun scripts/e2e-server.ts',
      url: baseURL + '/login',
      timeout: 120_000,
      gracefulShutdown: { signal: 'SIGTERM', timeout: 10_000 },
    },
  }),
})
