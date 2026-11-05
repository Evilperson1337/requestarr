// Lets the page's timers run for `ms` of fake time (the test must have called
// page.clock.install() before page.goto), then makes one round trip through
// the mocked API. Any request a timer started would have reached the route
// handler first, so a "nothing was requested" assertion after this is sound
// without a real sleep.
export async function letTimersRun(page: import('@playwright/test').Page, ms: number) {
    await page.clock.runFor(ms)
    await page.evaluate(() => fetch('/api/v1/search/types', { cache: 'no-store' }).then(() => undefined))
}
