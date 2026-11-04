import { expect, test, type Page } from '@playwright/test'

async function mockApp(page: Page) {
    await page.route('**/api/v1/**', route => {
        const path = new URL(route.request().url()).pathname.replace('/api/v1', '')
        if (path === '/setup/status') return route.fulfill({ json: { complete: true, required: false, wizardComplete: true } })
        if (path === '/auth/me') return route.fulfill({ json: { user: { id: 'user-1', role: 'requester', email: 'user@example.test', displayName: 'User' }, csrfToken: 'csrf-token' } })
        return route.fulfill({ json: { items: [], nextCursor: null } })
    })
}

const sentence = 'This application uses TMDB and the TMDB APIs but is not endorsed, certified, or otherwise approved by TMDB.'

for (const path of ['/', '/library', '/requests']) {
    test(`TMDB attribution is visible on ${path}`, async ({ page }) => {
        await mockApp(page)
        await page.goto(path)
        const notice = page.getByRole('contentinfo')
        await expect(notice).toContainText(sentence)
        const logo = notice.getByRole('img', { name: 'The Movie Database (TMDB)' })
        await expect(logo).toBeVisible()
        expect(await logo.evaluate((img: HTMLImageElement) => img.naturalWidth)).toBeGreaterThan(0)
    })
}

test('TMDB attribution does not overflow or hide behind the bottom bar at phone width', async ({ page }) => {
    await page.setViewportSize({ width: 360, height: 640 })
    await mockApp(page)
    await page.goto('/')
    const notice = page.getByRole('contentinfo')
    await expect(notice.getByRole('img')).toBeVisible()
    await page.evaluate(() => window.scrollTo(0, document.documentElement.scrollHeight))
    await page.waitForTimeout(200)
    const box = (await notice.boundingBox())!
    const nav = (await page.getByRole('navigation', { name: 'Main navigation' }).boundingBox())!
    expect(box.x).toBeGreaterThanOrEqual(0)
    expect(box.x + box.width).toBeLessThanOrEqual(360)
    expect(box.y + box.height).toBeLessThanOrEqual(nav.y)
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(360)
})
