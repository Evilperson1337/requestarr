import { expect, test, type Locator, type Page } from '@playwright/test'

test.use({ hasTouch: true })

// Forty films is more than any row fits, so the rail always has a second page.
const films = Array.from({ length: 40 }, (_, i) => ({
    id: `f${i + 1}`, title: `Film ${i + 1}`, year: 2000 + (i % 20), mediaType: 'movie', source: 'Plex', addedAt: '2026-11-01T12:00:00Z',
}))

async function openHome(page: Page) {
    await page.route('**/api/v1/**', route => {
        const url = new URL(route.request().url())
        const path = url.pathname.replace('/api/v1', '')
        if (path === '/setup/status') return route.fulfill({ json: { complete: true, required: false, wizardComplete: true } })
        if (path === '/auth/me') return route.fulfill({ json: { user: { id: 'user-1', role: 'requester', displayName: 'User' }, csrfToken: 'csrf-token' } })
        if (path === '/requests') return route.fulfill({ json: { items: [], nextCursor: null } })
        if (path === '/library/recent') return route.fulfill({ json: { items: url.searchParams.get('types') === 'movie,tv' ? films : [] } })
        if (path === '/search/types') return route.fulfill({ json: { types: [] } })
        return route.fulfill({ status: 404, json: { error: { code: 'not_found', message: 'unmocked' } } })
    })
    await page.goto('/')
    const shelf = page.getByRole('region', { name: 'New movies and shows in your libraries' })
    await expect(titles(shelf).first()).toHaveText('Film 1')
    return shelf
}

const titles = (shelf: Locator) => shelf.locator('.railTitle')

// Playwright's touchscreen can only tap, so a drag is sent as DevTools touch events.
async function drag(page: Page, shelf: Locator, by: { dx: number; dy: number }) {
    const box = await shelf.locator('.coverRail').boundingBox()
    if (!box) throw new Error('the rail has no box')
    const from = { x: box.x + box.width / 2, y: box.y + box.height / 2 }
    const cdp = await page.context().newCDPSession(page)
    const at = (x: number, y: number) => [{ x, y }]
    await cdp.send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: at(from.x, from.y) })
    await cdp.send('Input.dispatchTouchEvent', { type: 'touchMove', touchPoints: at(from.x + by.dx / 2, from.y + by.dy / 2) })
    await cdp.send('Input.dispatchTouchEvent', { type: 'touchMove', touchPoints: at(from.x + by.dx, from.y + by.dy) })
    await cdp.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] })
    // React applies the page change on the next frame; two frames make "nothing changed" a real check.
    await page.evaluate(() => new Promise(done => requestAnimationFrame(() => requestAnimationFrame(done))))
}

test('swiping left pages the library rail to older items and swiping right comes back', async ({ page }) => {
    const shelf = await openHome(page)
    const newer = shelf.getByRole('button', { name: /^Newer/ })
    const perPage = await titles(shelf).count()
    await expect(newer).toBeDisabled()

    await drag(page, shelf, { dx: -120, dy: 4 })
    await expect(titles(shelf).first()).toHaveText(`Film ${perPage + 1}`)
    await expect(newer).toBeEnabled()

    await drag(page, shelf, { dx: 120, dy: 4 })
    await expect(titles(shelf).first()).toHaveText('Film 1')
    await expect(newer).toBeDisabled()
})

test('a short swipe, or one that is mostly vertical, does not page the rail', async ({ page }) => {
    const shelf = await openHome(page)
    await drag(page, shelf, { dx: -30, dy: 0 })
    await expect(titles(shelf).first()).toHaveText('Film 1')
    await drag(page, shelf, { dx: -60, dy: 90 })
    await expect(titles(shelf).first()).toHaveText('Film 1')
    await expect(shelf.getByRole('button', { name: /^Newer/ })).toBeDisabled()
})

test('swiping right on the newest page, and swiping left on the last page, stay put', async ({ page }) => {
    const shelf = await openHome(page)
    await drag(page, shelf, { dx: 120, dy: 0 })
    await expect(titles(shelf).first()).toHaveText('Film 1')

    const older = shelf.getByRole('button', { name: /^Older/ })
    while (await older.isEnabled()) await older.click()
    const last = await titles(shelf).first().innerText()
    expect(last).not.toBe('Film 1')
    await drag(page, shelf, { dx: -120, dy: 0 })
    await expect(titles(shelf).first()).toHaveText(last)
    await expect(older).toBeDisabled()
})

test('the arrow buttons still page the rail', async ({ page }) => {
    const shelf = await openHome(page)
    const perPage = await titles(shelf).count()
    await shelf.getByRole('button', { name: /^Older/ }).click()
    await expect(titles(shelf).first()).toHaveText(`Film ${perPage + 1}`)
    await shelf.getByRole('button', { name: /^Newer/ }).click()
    await expect(titles(shelf).first()).toHaveText('Film 1')
})
