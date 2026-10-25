// @ts-check
import { defineConfig, devices } from '@playwright/test';

const PORT = 4173;
// The test-report tool (go run -C tools . test-report) sets E2E_REPORT_DIR: JUnit for the report, JSON for the flaky count.
const reportDir = process.env.E2E_REPORT_DIR;

/**
 * Black-box regression of the UI in ui/, driven through ui/dev-mock.js (the
 * browser stand-in for the Tauri bridge). Defaults from `npm init playwright`,
 * plus WebKit, the engine the Tauri window uses on macOS.
 * @see https://playwright.dev/docs/test-configuration
 */
export default defineConfig({
  testDir: './e2e',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  workers: process.env.CI ? 1 : undefined,
  reporter: reportDir
    ? [['list'], ['html', { open: 'never' }], ['junit', { outputFile: `${reportDir}/e2e-junit.xml` }], ['json', { outputFile: `${reportDir}/e2e.json` }]]
    : 'html',
  use: {
    baseURL: `http://127.0.0.1:${PORT}`,
    trace: 'on-first-retry',
  },
  projects: [
    { name: 'chromium', use: { ...devices['Desktop Chrome'] } },
    { name: 'webkit', use: { ...devices['Desktop Safari'] } },
  ],
  webServer: {
    command: `PORT=${PORT} bun e2e/support/serve-ui.js`,
    url: `http://127.0.0.1:${PORT}`,
    reuseExistingServer: !process.env.CI,
  },
});
