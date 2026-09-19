import { defineConfig } from '@playwright/test'
export default defineConfig({
  testDir: './tests/e2e',
  fullyParallel: true,
  workers: 3,
  timeout: 30_000,
  use: { baseURL: 'http://127.0.0.1:5198', viewport: { width: 1440, height: 900 }, trace: 'retain-on-failure' },
  projects: [
    { name: 'chromium', use: { browserName: 'chromium' } },
    { name: 'webkit', use: { browserName: 'webkit' } },
  ],
  webServer: {
    command: process.env.SONEXA_E2E_PREVIEW
      ? 'npm run preview -- --host 127.0.0.1 --port 5198 --strictPort'
      : 'npm run dev -- --host 127.0.0.1 --port 5198 --strictPort',
    url: 'http://127.0.0.1:5198', reuseExistingServer: false,
  },
})
