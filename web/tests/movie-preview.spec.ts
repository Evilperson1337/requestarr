import { expect, test, type Page } from '@playwright/test'
import AxeBuilder from '@axe-core/playwright'
import { capabilitiesFor } from './capabilities'

const movie = {
    Item: { ID: '11111111-1111-4111-8111-111111111111', MediaType: 'movie', Provider: 'tmdb', Title: 'Fixture movie', Year: 2026, ArtworkURL: '', ArtworkKind: 'poster', ExternalURL: 'https://www.themoviedb.org/movie/1762506' }, Platforms: [],
    Movie: { overview: 'An official synopsis. '.repeat(35), runtime: 145, genres: ['Adventure'], releaseDate: '2026-01-01', releaseStatus: 'Released', score: 7.8, ratingCount: 120 },
    Preview: { availability: 'unknown', canRequest: true, canSubscribe: false },
}

async function openMovie(page: Page, resolved: unknown = movie, role = 'requester') {
    await page.route('**/api/v1/**', route => {
        const path = new URL(route.request().url()).pathname
        if (path.endsWith('/auth/me')) return route.fulfill({ json: { user: { id: 'user-1', role, displayName: 'User' }, capabilities: capabilitiesFor(role), csrfToken: 'csrf-token' } })
        if (path.endsWith('/resolve')) return route.fulfill({ json: resolved })
        return route.fulfill({ json: { items: [], nextCursor: null } })
    })
    await page.goto('/')
    await page.getByPlaceholder('Search or paste a link…').fill('https://www.themoviedb.org/movie/1762506-band-of-brothers-legacy')
    await page.getByRole('button', { name: 'Preview', exact: true }).click()
    return page.getByRole('dialog', { name: /Fixture movie/ })
}

test('movie preview exposes rich facts, genre tags and the whole official synopsis', async ({ page }) => {
    const dialog = await openMovie(page)
    await expect(dialog).toContainText('2h 25m')
    await expect(dialog).toContainText('Adventure')
    await expect(dialog).toContainText('7.8/10')
    await expect(dialog).toContainText('120 ratings')
    // The synopsis is shown whole; there is nothing to expand.
    await expect(dialog.getByRole('button', { name: /Show (more|less)/ })).toHaveCount(0)
    await expect(dialog.getByRole('list', { name: 'Genres' }).getByRole('listitem')).toHaveText(['Adventure'])
    await expect(dialog.getByRole('link', { name: 'TMDB (opens in a new tab)' })).toHaveAttribute('href', movie.Item.ExternalURL)
})

test('movie scrollbar uses the dark theme and respects forced system colors', async ({ page }) => {
    const dialog = await openMovie(page)
    const scroll = dialog.locator('.movieScroll')
    await expect(scroll).toHaveCSS('scrollbar-color', 'rgb(68, 68, 68) rgb(17, 17, 17)')
    await expect(dialog).toHaveCSS('color-scheme', 'dark')
    await page.emulateMedia({ forcedColors: 'active' })
    await expect(scroll).toHaveCSS('scrollbar-color', 'auto')
})

test('movie request preserves a failed note and confirms success without navigating', async ({ page }) => {
    const dialog = await openMovie(page)
    let attempts = 0
    await page.route('**/api/v1/requests', route => {
        attempts++
        expect(route.request().postDataJSON().note).toBe('Please add this')
        if (attempts === 1) return route.fulfill({ status: 503, json: { error: { code: 'unavailable', message: 'Please retry' } } })
        return route.fulfill({ json: { Created: true, Request: { ID: '22222222-2222-4222-8222-222222222222', Status: 'requested' } } })
    })
    await page.route('**/api/v1/catalog/*/preview', route => route.fulfill({ json: { availability: 'unknown', canRequest: false, canSubscribe: false, request: { id: '22222222-2222-4222-8222-222222222222', status: 'requested', subscribed: true } } }))
    await dialog.getByRole('button', { name: 'Add a note' }).click()
    await dialog.getByLabel('Note (optional)').fill('Please add this')
    await dialog.getByRole('button', { name: 'Request movie', exact: true }).click()
    await expect(dialog.getByRole('alert')).toContainText('Please retry')
    await expect(dialog.getByLabel('Note (optional)')).toHaveValue('Please add this')
    await dialog.getByRole('button', { name: 'Request movie', exact: true }).click()
    await expect(dialog).toContainText('Movie requested')
    await expect(dialog).toContainText('Notifications On')
    await expect(page).toHaveURL(/^http:\/\/127\.0\.0\.1:\d+\/$/)
    await expect(dialog.getByRole('button', { name: 'Request movie', exact: true })).toHaveCount(0)
    await expect(dialog.getByRole('link', { name: 'View request' })).toHaveAttribute('href', '/requests?requestId=22222222-2222-4222-8222-222222222222')
})

test('credits and partial failures remain useful without a gallery', async ({ page }) => {
    const dialog = await openMovie(page, { ...movie, Movie: { ...movie.Movie, directors: ['Fixture Director'], cast: [{ name: 'Fixture Actor', character: 'The lead' }], detailsUnavailable: true } })
    await expect(dialog).toContainText('Fixture Director')
    await expect(dialog.getByRole('region', { name: 'Lead cast' })).toContainText('Fixture Actor')
    await expect(dialog).toContainText('The lead')
    await expect(dialog).toContainText('Some details unavailable')
    await expect(dialog.getByRole('button', { name: 'Request movie', exact: true })).toBeEnabled()
})

test('existing shared Request offers notifications and an exact target, not creation', async ({ page }) => {
    const dialog = await openMovie(page, { ...movie, Preview: { availability: 'unknown', canRequest: false, canSubscribe: true, request: { id: '22222222-2222-4222-8222-222222222222', status: 'requested', subscribed: false } } })
    await page.route('**/api/v1/requests/*/subscription', route => route.fulfill({ json: { subscribed: true, changed: true } }))
    await expect(dialog.getByRole('button', { name: 'Request movie', exact: true })).toHaveCount(0)
    await dialog.getByRole('button', { name: 'Notify Me' }).click()
    await expect(dialog).toContainText('Notifications On')
    await dialog.getByRole('link', { name: 'View request' }).click()
    await expect(page).toHaveURL(/\/requests\?requestId=22222222-2222-4222-8222-222222222222$/)
})

test('known availability offers the library without creating a Request', async ({ page }) => {
    const dialog = await openMovie(page, { ...movie, Preview: { availability: 'available', canRequest: false, canSubscribe: false, libraries: [{ name: 'Library', url: 'https://library.example.test/items/1' }] } })
    await expect(dialog.getByRole('list', { name: 'Library availability' })).toContainText('Library: Available')
    await expect(dialog.getByRole('link', { name: 'Library: Available' })).toHaveAttribute('href', 'https://library.example.test/items/1')
    await expect(dialog.getByRole('button', { name: 'Request movie', exact: true })).toHaveCount(0)
})

test('sparse upcoming movie remains requestable on a phone and keyboard close restores focus', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 700 })
    const dialog = await openMovie(page, { ...movie, Movie: { releaseStatus: 'Planned' } })
    await expect(dialog).toContainText('Upcoming')
    await expect(dialog).toContainText('Release date unknown')
    await expect(dialog).toContainText('Not yet rated')
    await expect(dialog).not.toContainText('0m')
    await expect(dialog.getByRole('img', { name: 'Poster unavailable' })).toBeVisible()
    await expect(dialog.getByRole('button', { name: 'Request movie', exact: true })).toBeInViewport()
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    await page.keyboard.press('Escape')
    await expect(dialog).toHaveCount(0)
    await expect(page.getByRole('button', { name: 'Preview', exact: true })).toBeFocused()
})

test('viewer cannot create a Request', async ({ page }) => {
    const dialog = await openMovie(page, movie, 'viewer')
    await expect(dialog.getByRole('button', { name: 'Request movie', exact: true })).toHaveCount(0)
    await expect(dialog).toContainText('Your viewer role cannot create Requests. Ask an administrator to make you a requester.')
})

test('authorization loss removes protected actions and does not retry a stale note', async ({ page }) => {
    const dialog = await openMovie(page)
    await page.route('**/api/v1/requests', route => route.fulfill({ status: 403, json: { error: { code: 'forbidden', message: 'Access changed. Sign in again.' } } }))
    await dialog.getByRole('button', { name: 'Add a note' }).click()
    await dialog.getByLabel('Note (optional)').fill('Private note')
    await dialog.getByRole('button', { name: 'Request movie', exact: true }).click()
    await expect(dialog.getByRole('alert')).toContainText('Access changed')
    await expect(dialog.getByRole('region', { name: 'Request and availability' })).toHaveCount(0)
    await expect(dialog.getByLabel('Note (optional)')).toHaveCount(0)
    await expect(dialog.getByRole('button', { name: 'Request movie', exact: true })).toHaveCount(0)
})

test('a concurrent creator yields the existing Request without a false subscription claim', async ({ page }) => {
    const dialog = await openMovie(page)
    await page.route('**/api/v1/requests', route => route.fulfill({ json: { Created: false, Request: { ID: '22222222-2222-4222-8222-222222222222', Status: 'requested' } } }))
    await page.route('**/api/v1/catalog/*/preview', route => route.fulfill({ json: { availability: 'unknown', canRequest: false, canSubscribe: true, request: { id: '22222222-2222-4222-8222-222222222222', status: 'requested', subscribed: false } } }))
    await dialog.getByRole('button', { name: 'Request movie', exact: true }).click()
    await expect(dialog).toContainText('This movie was already requested')
    await expect(dialog).not.toContainText('Notifications On')
    await expect(dialog.getByRole('button', { name: 'Notify Me' })).toBeVisible()
})

test('reopening the same movie reuses metadata and refreshes only local state', async ({ page }) => {
    const dialog = await openMovie(page)
    let providerLookups = 0
    await page.route('**/api/v1/resolve', route => { providerLookups++; return route.fulfill({ json: movie }) })
    await page.route('**/api/v1/catalog/*/preview', route => route.fulfill({ json: movie.Preview }))
    await dialog.getByRole('button', { name: 'Close preview' }).click()
    await page.getByRole('button', { name: 'Preview', exact: true }).click()
    await expect(dialog).toBeVisible()
    expect(providerLookups).toBe(0)
    await dialog.getByRole('button', { name: 'Close preview' }).click()
    const input = page.getByPlaceholder('Search or paste a link…')
    await input.focus()
    await input.press('Enter')
    await expect(dialog).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(dialog).toHaveCount(0)
    await expect(input).toBeFocused()
    expect(providerLookups).toBe(0)
})

test('movie preview is keyboard-contained and has no serious accessibility violations', async ({ page }) => {
    const dialog = await openMovie(page)
    const results = await new AxeBuilder({ page }).include('[role="dialog"]').analyze()
    expect(results.violations.filter(violation => violation.impact === 'serious' || violation.impact === 'critical')).toEqual([])
    await dialog.getByRole('button', { name: 'Request movie', exact: true }).focus()
    await page.keyboard.press('Tab')
    await expect(dialog.getByRole('button', { name: 'Close preview' })).toBeFocused()
    await page.keyboard.press('Shift+Tab')
    await expect(dialog.getByRole('button', { name: 'Request movie', exact: true })).toBeFocused()
})

test('in-flight submission is disabled and only one creation call is made', async ({ page }) => {
    const dialog = await openMovie(page)
    let release!: () => void
    const gate = new Promise<void>(resolve => { release = resolve })
    let calls = 0
    await page.route('**/api/v1/requests', async route => { calls++; await gate; await route.fulfill({ json: { Created: true, Request: { ID: '22222222-2222-4222-8222-222222222222', Status: 'requested' } } }) })
    await page.route('**/api/v1/catalog/*/preview', route => route.fulfill({ json: { availability: 'unknown', canRequest: false, canSubscribe: false, request: { id: '22222222-2222-4222-8222-222222222222', status: 'requested', subscribed: true } } }))
    await dialog.getByRole('button', { name: 'Request movie', exact: true }).click()
    await expect(dialog.getByRole('button', { name: 'Submitting…', exact: true })).toBeDisabled()
    await expect.poll(() => calls).toBe(1)
    release()
    await expect(dialog).toContainText('Movie requested')
    expect(calls).toBe(1)
})

test('availability reads as one tag per media server, linked to the item', async ({ page }) => {
    const dialog = await openMovie(page, { ...movie, Preview: { availability: 'available', canRequest: false, canSubscribe: false, libraries: [{ name: 'Plex', url: 'https://plex.example.test/item/1' }, { name: 'Jellyfin', url: 'https://jellyfin.example.test/item/9' }] } })
    const tags = dialog.getByRole('list', { name: 'Library availability' })
    await expect(tags.getByRole('listitem')).toHaveText(['Plex: Available', 'Jellyfin: Available'])
    await expect(tags.getByRole('link', { name: 'Jellyfin: Available' })).toHaveAttribute('href', 'https://jellyfin.example.test/item/9')
    await expect(dialog.getByText('Library availability unknown')).toHaveCount(0)
})

test('without library evidence the availability tag says so plainly', async ({ page }) => {
    const unknown = await openMovie(page)
    await expect(unknown.getByRole('list', { name: 'Library availability' })).toHaveText('Availability unknown')
})

test('movie dialog has one close control in its top-right corner, cast photos and a TMDB link', async ({ page }) => {
    const photo = 'data:image/gif;base64,R0lGODlhAQABAAAAACH5BAEKAAEALAAAAAABAAEAAAICTAEAOw=='
    const dialog = await openMovie(page, { ...movie, Movie: { ...movie.Movie, cast: [{ name: 'Pictured Actor', character: 'Lead', profileUrl: photo }, { name: 'Plain Actor' }] } })
    // No separate Cancel button; the corner X is the only way out besides Escape.
    await expect(dialog.getByRole('button', { name: 'Cancel' })).toHaveCount(0)
    const box = await dialog.boundingBox(), close = await dialog.getByRole('button', { name: 'Close preview' }).boundingBox()
    expect(box!.x + box!.width - (close!.x + close!.width)).toBeLessThan(24)
    expect(close!.y - box!.y).toBeLessThan(24)
    const cast = dialog.getByRole('region', { name: 'Lead cast' })
    await expect(cast.locator('img.castPhoto')).toHaveCount(1)
    await expect(cast.locator('.castInitials')).toHaveText('PA')
    // The rating line reads as one sentence with the source as its link.
    await expect(dialog.locator('.movieRating')).toHaveText('TMDB 7.8/10 · 120 ratings')
    await expect(dialog.locator('.movieRating a')).toHaveCount(1)
    await expect(dialog.getByText('Open on TMDB')).toHaveCount(0)
})

test('a single rating reads "1 rating"', async ({ page }) => {
    const dialog = await openMovie(page, { ...movie, Movie: { ...movie.Movie, score: 5, ratingCount: 1 } })
    await expect(dialog.locator('.movieRating')).toHaveText('TMDB 5.0/10 · 1 rating')
})

test('a movie a library already holds names that library and cannot be requested', async ({ page }) => {
    const dialog = await openMovie(page, { ...movie, Preview: { availability: 'available', canRequest: false, canSubscribe: false, canExtend: false, libraries: [{ name: 'Plex' }] } })
    await expect(dialog.getByRole('list', { name: 'Library availability' }).getByRole('listitem')).toHaveText(['Plex: Available'])
    await expect(dialog.getByRole('link', { name: /Plex/ })).toHaveCount(0)
    await expect(dialog.getByRole('button', { name: 'Request movie' })).toHaveCount(0)
})
