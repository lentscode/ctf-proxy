import { defineConfig, devices } from '@playwright/test'

const baseURL = process.env.LAB_BASE_URL
if (!baseURL) throw new Error('LAB_BASE_URL is required for the real-world lab')

export default defineConfig({
  testDir: '.',
  workers: 1,
  retries: 0,
  reporter: 'list',
  use: { baseURL, trace: 'retain-on-failure' },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
})
