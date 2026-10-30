import { expect, test, type Page } from '@playwright/test'
import AxeBuilder from '@axe-core/playwright'

const movie = {
    Item: { ID: '11111111-1111-4111-8111-111111111111', MediaType: 'movie', Provider: 'tmdb', Title: 'Fixture movie', Year: 2026, ArtworkURL: '', ArtworkKind: 'poster', ExternalURL: 'https://www.themoviedb.org/movie/1762506' }, Platforms: [],
    Movie: { overview: 'An official synopsis. '.repeat(35), runtime: 145, genres: ['Adventure'], releaseDate: '2026-01-01', releaseStatus: 'Released', score: 7.8, ratingCount: 120 },
    Preview: { availability: 'unknown', canRequest: true, canSubscribe: false },
}

async function openMovie(page: Page, resolved: unknown = movie, role = 'requester') {
    await page.route('**/api/v1/**', route => {
        const path = new URL(route.request().url()).pathname
        if (path.endsWith('/setup/status')) return route.fulfill({ json: { complete: true, required: false, wizardComplete: true } })
        if (path.endsWith('/auth/me')) return route.fulfill({ json: { user: { id: 'user-1', role, displayName: 'User' }, csrfToken: 'csrf-token' } })
        if (path.endsWith('/resolve')) return route.fulfill({ json: resolved })
        return route.fulfill({ json: { items: [], nextCursor: null } })
    })
    await page.goto('/')
    await page.getByPlaceholder('Paste a supported link…').fill('https://www.themoviedb.org/movie/1762506-band-of-brothers-legacy')
    await page.getByRole('button', { name: 'Preview', exact: true }).click()
    return page.getByRole('dialog', { name: /Fixture movie/ })
}

test('movie preview exposes rich facts and an expandable official synopsis', async ({ page }) => {
    const dialog = await openMovie(page)
    await expect(dialog).toContainText('2h 25m')
    await expect(dialog).toContainText('Adventure')
    await expect(dialog).toContainText('7.8/10')
    await expect(dialog).toContainText('120 ratings')
    await expect(dialog.getByRole('button', { name: 'Show more' })).toBeVisible()
    await dialog.getByRole('button', { name: 'Show more' }).click()
    await expect(dialog.getByRole('button', { name: 'Show less' })).toHaveAttribute('aria-expanded', 'true')
    await expect(dialog.getByRole('link', { name: 'Open on TMDB' })).toHaveAttribute('href', movie.Item.ExternalURL)
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
    await expect(page).toHaveURL('http://127.0.0.1:4173/')
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
    await expect(dialog).toContainText('Available in your libraries')
    await expect(dialog.getByRole('link', { name: 'Open in Library' })).toHaveAttribute('href', 'https://library.example.test/items/1')
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
    await expect(dialog).toContainText('Your viewer role cannot create Requests')
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
    await dialog.getByRole('button', { name: 'Cancel', exact: true }).click()
    await page.getByRole('button', { name: 'Preview', exact: true }).click()
    await expect(dialog).toBeVisible()
    expect(providerLookups).toBe(0)
    await dialog.getByRole('button', { name: 'Cancel', exact: true }).click()
    const input = page.getByPlaceholder('Paste a supported link…')
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
