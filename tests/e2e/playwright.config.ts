import { defineConfig } from '@playwright/test'

// These tests drive the REAL desktop app: RUNTIDE.exe is started with WebView2
// remote debugging enabled and Playwright attaches over CDP. No browser is
// downloaded and nothing is mocked.
export default defineConfig({
  testDir: '.',
  timeout: 120_000,
  workers: 1,
  fullyParallel: false,
  reporter: [['list']],
  use: { trace: 'off' },
})
