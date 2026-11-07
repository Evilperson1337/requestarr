import { expect, test, type Page } from '@playwright/test'
import AxeBuilder from '@axe-core/playwright'
import { capabilitiesFor } from './capabilities'

const series = {
    Item: { ID: '11111111-1111-4111-8111-111111111111', MediaType: 'tv', Provider: 'tmdb', Title: 'Fixture series', Year: 2020, ArtworkURL: '', ArtworkKind: 'poster', ExternalURL: 'https://www.themoviedb.org/tv/714' },
    Platforms: [],
    TV: { overview: 'The official series synopsis. '.repeat(25), episodeRuntime: 45, genres: ['Drama'], firstAirDate: '2020-01-02', lastAirDate: '2025-03-04', seriesStatus: 'Returning Series', seasons: 3, episodes: 30, score: 8.2, ratingCount: 400, creators: ['Fixture Creator'], cast: [{ name: 'Fixture Actor', character: 'The lead' }] },
    Preview: { availability: 'unknown', canRequest: true, canSubscribe: false },
}

async function openSeries(page: Page, resolved: unknown = series, role = 'requester') {
    await page.route('**/api/v1/**', route => {
        const path = new URL(route.request().url()).pathname
        if (path.endsWith('/auth/me')) return route.fulfill({ json: { user: { id: 'user-1', role, displayName: 'User' }, capabilities: capabilitiesFor(role), csrfToken: 'csrf-token' } })
        if (path.endsWith('/resolve')) return route.fulfill({ json: resolved })
        if (path.includes('/catalog/')) return route.fulfill({ json: series.Preview })
        return route.fulfill({ json: { items: [], nextCursor: null } })
    })
    await page.goto('/')
    await page.getByPlaceholder('Search or paste a link…').fill('https://www.themoviedb.org/tv/714-fixture-series')
    await page.getByRole('button', { name: 'Preview', exact: true }).click()
    return page.getByRole('dialog', { name: /Fixture series/ })
}

test('TV preview shows series facts, creators, cast, synopsis, and the themed scrollbar', async ({ page }) => {
    const dialog = await openSeries(page)
    await expect(dialog).toContainText('TV preview')
    await expect(dialog).toContainText('Returning Series')
    await expect(dialog).toContainText('45m per episode')
    await expect(dialog).toContainText('3 seasons')
    await expect(dialog).toContainText('30 episodes')
    await expect(dialog).toContainText('First aired:')
    await expect(dialog).toContainText('Last aired:')
    await expect(dialog.locator('time').first()).toHaveAttribute('datetime', '2020-01-02')
    await expect(dialog).toContainText('Creator: Fixture Creator')
    await expect(dialog).not.toContainText('Director:')
    await expect(dialog).toContainText('Fixture Actor')
    await expect(dialog).toContainText('8.2/10')
    await expect(dialog.getByRole('button', { name: /Show (more|less)/ })).toHaveCount(0)
    await expect(dialog.locator('.movieScroll')).toHaveCSS('scrollbar-color', 'rgb(68, 68, 68) rgb(17, 17, 17)')
    await expect(dialog.getByRole('button', { name: 'Request series', exact: true })).toBeVisible()
    const accessibility = await new AxeBuilder({ page }).include('[role="dialog"]').analyze()
    expect(accessibility.violations.filter(violation => violation.impact === 'serious' || violation.impact === 'critical')).toEqual([])
})

test('TV submits one whole-series Request and confirms it in the modal', async ({ page }) => {
    const dialog = await openSeries(page)
    const calls: unknown[] = []
    await page.route('**/api/v1/requests', route => {
        const body = route.request().postDataJSON()
        calls.push(body)
        expect(body).toEqual({ catalogId: series.Item.ID, note: 'The whole series, please', platforms: { any: true } })
        return route.fulfill({ json: { Created: true, Request: { ID: '22222222-2222-4222-8222-222222222222', Status: 'requested' } } })
    })
    await page.route('**/api/v1/catalog/*/preview', route => route.fulfill({ json: { availability: 'unknown', canRequest: false, canSubscribe: false, request: { id: '22222222-2222-4222-8222-222222222222', status: 'requested', subscribed: true } } }))
    await dialog.getByRole('button', { name: 'Add a note' }).click()
    await dialog.getByLabel('Note (optional)').fill('The whole series, please')
    await expect(dialog.getByRole('combobox')).toHaveCount(0)
    await expect(dialog.getByRole('checkbox')).toHaveCount(0)
    await dialog.getByRole('button', { name: 'Request series', exact: true }).click()
    await expect(dialog).toContainText('Series requested')
    await expect(dialog).toContainText('Notifications On')
    await expect(page).toHaveURL(/^http:\/\/127\.0\.0\.1:\d+\/$/)
    expect(calls).toHaveLength(1)
    await expect(dialog.getByRole('link', { name: 'View request' })).toHaveAttribute('href', '/requests?requestId=22222222-2222-4222-8222-222222222222')
})

test('sparse upcoming TV remains usable on phones and reopening does not refetch TMDB', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 700 })
    const dialog = await openSeries(page, { ...series, TV: { seriesStatus: 'Planned', detailsUnavailable: true } })
    await expect(dialog).toContainText('Upcoming')
    await expect(dialog).toContainText('First air date unknown')
    await expect(dialog).toContainText('Not yet rated')
    await expect(dialog).toContainText('Some details unavailable')
    await expect(dialog).not.toContainText('0 seasons')
    await expect(dialog.getByRole('button', { name: 'Request series', exact: true })).toBeInViewport()
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    let lookups = 0
    await page.route('**/api/v1/resolve', route => { lookups++; return route.fulfill({ json: series }) })
    await page.keyboard.press('Escape')
    await expect(page.getByRole('button', { name: 'Preview', exact: true })).toBeFocused()
    await page.getByRole('button', { name: 'Preview', exact: true }).click()
    await expect(dialog).toBeVisible()
    expect(lookups).toBe(0)
})

test('existing ended series offers permitted notifications instead of a duplicate Request', async ({ page }) => {
    const dialog = await openSeries(page, { ...series, TV: { ...series.TV, firstAirDate: undefined, seriesStatus: 'Ended' }, Preview: { availability: 'unknown', canRequest: false, canSubscribe: true, request: { id: '22222222-2222-4222-8222-222222222222', status: 'requested', subscribed: false } } }, 'viewer')
    await expect(dialog).toContainText('Ended')
    await expect(dialog).not.toContainText('Upcoming')
    await expect(dialog.getByRole('button', { name: 'Request series', exact: true })).toHaveCount(0)
    await page.route('**/api/v1/requests/*/subscription', route => route.fulfill({ json: { subscribed: true, changed: true } }))
    await dialog.getByRole('button', { name: 'Notify Me' }).click()
    await expect(dialog).toContainText('Notifications On')
    await expect(dialog.getByRole('link', { name: 'View request' })).toHaveAttribute('href', '/requests?requestId=22222222-2222-4222-8222-222222222222')
})
