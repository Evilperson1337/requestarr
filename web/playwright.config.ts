import { defineConfig } from '@playwright/test'

// PLAYWRIGHT_PORT lets two checkouts run the browser tests at the same time.
const port = (globalThis as { process?: { env: Record<string, string | undefined> } }).process?.env.PLAYWRIGHT_PORT ?? '4173'

export default defineConfig({
    testDir: './tests',
    fullyParallel: true,
    workers: 2,
    use: {
        baseURL: `http://127.0.0.1:${port}`,
        browserName: 'chromium',
        screenshot: 'only-on-failure',
        trace: 'retain-on-failure',
    },
    webServer: {
        command: `npm run dev -- --host 127.0.0.1 --port ${port} --strictPort`,
        url: `http://127.0.0.1:${port}`,
        reuseExistingServer: false,
    },
})
