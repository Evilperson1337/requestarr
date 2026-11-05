import AxeBuilder from '@axe-core/playwright'
import { expect, test, type Page } from '@playwright/test'
import { letTimersRun } from './fixtures'

const box = 'Search or paste a link…'
const dune = { mediaType: 'movie', provider: 'tmdb', providerId: '438631', ref: 'https://www.themoviedb.org/movie/438631', title: 'Dune', year: 2021, artworkUrl: '', artworkKind: 'poster' }
const prophecy = { mediaType: 'tv', provider: 'tmdb', providerId: '90000', ref: 'https://www.themoviedb.org/tv/90000', title: 'Dune: Prophecy', year: 2024, artworkUrl: '', artworkKind: 'poster' }
const resolved = {
    Item: { ID: '11111111-1111-4111-8111-111111111111', MediaType: 'movie', Provider: 'tmdb', ProviderID: '438631', Title: 'Dune', Year: 2021, ArtworkURL: '', ArtworkKind: 'poster', ExternalURL: 'https://www.themoviedb.org/movie/438631' }, Platforms: [],
    Movie: { overview: 'Spice.', genres: [] },
    Preview: { availability: 'unknown', canRequest: true, canSubscribe: false },
}

type Reply = { status?: number; json: unknown; delay?: number }
type Options = { types?: string[]; role?: string; search?: (url: URL) => Reply }

// Records every search and resolve the page makes. The dev server runs effects
// twice, so tests assert on distinct values, not raw counts, where that matters.
async function openHome(page: Page, options: Options = {}) {
    const searches: string[] = []
    const served: string[] = []
    const resolves: string[] = []
    await page.route('**/api/v1/**', async route => {
        const url = new URL(route.request().url())
        const path = url.pathname.replace('/api/v1', '')
        if (path === '/setup/status') return route.fulfill({ json: { complete: true, required: false } })
        if (path === '/auth/me') return route.fulfill({ json: { user: { id: 'user-1', role: options.role ?? 'requester', displayName: 'User' }, csrfToken: 'csrf-token' } })
        if (path === '/search/types') return route.fulfill({ json: { types: options.types ?? ['movie_tv'] } })
        if (path === '/search') {
            searches.push(url.searchParams.get('q') ?? '')
            const reply = options.search?.(url) ?? { json: { results: [dune, prophecy], hasMore: false } }
            if (reply.delay) await new Promise(resolve => setTimeout(resolve, reply.delay))
            served.push(url.searchParams.get('q') ?? '')
            return route.fulfill({ status: reply.status, json: reply.json }).catch(() => undefined)
        }
        if (path === '/resolve') { resolves.push(route.request().postDataJSON().url); return route.fulfill({ json: resolved }) }
        return route.fulfill({ json: { items: [], nextCursor: null } })
    })
    await page.clock.install()
    await page.goto('/')
    return { searches, resolves, served }
}

test('typing a title lists movie and TV results with year, then a click opens the same preview a link does', async ({ page }) => {
    const { searches, resolves } = await openHome(page)
    await page.getByPlaceholder(box).fill('dune')
    const results = page.getByRole('list', { name: 'Search results' })
    await expect(results.getByRole('button')).toHaveCount(2)
    await expect(results.getByRole('button', { name: /Dune.*2021/ }).first()).toBeVisible()
    await expect(results.getByRole('button', { name: /Dune: Prophecy.*2024/ })).toBeVisible()
    expect([...new Set(searches)]).toEqual(['dune'])
    await expect(page.getByRole('status')).toContainText('2 results for dune')

    await results.getByRole('button', { name: /^Dune 2021/ }).click()
    const dialog = page.getByRole('dialog', { name: /Dune/ })
    await expect(dialog).toBeVisible()
    expect(resolves).toEqual(['https://www.themoviedb.org/movie/438631'])
    // The list stays mounted behind the modal, and closing returns focus to the row.
    await expect(results).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(dialog).toHaveCount(0)
    await expect(results.getByRole('button', { name: /^Dune 2021/ })).toBeFocused()
})

test('a search request carries the type and text, waits for two characters and for a pause in typing', async ({ page }) => {
    const urls: URL[] = []
    const { searches } = await openHome(page, { search: url => { urls.push(url); return { json: { results: [], hasMore: false } } } })
    const input = page.getByPlaceholder(box)
    await input.fill('d')
    await letTimersRun(page, 600)
    expect(searches).toEqual([])
    await input.pressSequentially('une', { delay: 40 })
    await expect(page.getByText('No results for dune')).toBeVisible()
    expect(urls[0].searchParams.get('type')).toBe('movie_tv')
    expect([...new Set(searches)]).toEqual(['dune'])
})

test('a pasted link resolves as before and never searches, whatever the type says', async ({ page }) => {
    const { searches, resolves } = await openHome(page)
    await page.getByPlaceholder(box).fill('https://www.themoviedb.org/movie/438631-dune')
    await page.getByRole('button', { name: 'Preview', exact: true }).click()
    await expect(page.getByRole('dialog', { name: /Dune/ })).toBeVisible()
    expect(resolves).toEqual(['https://www.themoviedb.org/movie/438631-dune'])
    await letTimersRun(page, 500)
    expect(searches).toEqual([])
})

test('a bare audiobook ASIN is a link, not a search', async ({ page }) => {
    const { searches, resolves } = await openHome(page)
    await page.getByPlaceholder(box).fill('B08G9PRS1K')
    await page.getByRole('button', { name: 'Preview', exact: true }).click()
    await expect(page.getByRole('dialog')).toBeVisible()
    expect(resolves).toEqual(['B08G9PRS1K'])
    expect(searches).toEqual([])
})

test('a slow earlier search never overwrites the latest results', async ({ page }) => {
    const { searches, served } = await openHome(page, {
        search: url => url.searchParams.get('q') === 'dun'
            ? { json: { results: [{ ...dune, title: 'Slow stale answer', providerId: '1', ref: 'https://www.themoviedb.org/movie/1' }], hasMore: false }, delay: 1500 }
            : { json: { results: [{ ...dune, title: 'Fresh answer', providerId: '2', ref: 'https://www.themoviedb.org/movie/2' }], hasMore: false } },
    })
    const input = page.getByPlaceholder(box)
    await input.fill('dun')
    await expect.poll(() => searches).toContain('dun') // past the debounce, so the slow call is in flight
    await input.fill('dune')
    const results = page.getByRole('list', { name: 'Search results' })
    await expect(results.getByRole('button', { name: /Fresh answer/ })).toBeVisible()
    await expect.poll(() => served).toContain('dun') // the stale answer has now been delivered
    await expect(results.getByRole('button', { name: /Slow stale answer/ })).toHaveCount(0)
    await expect(results.getByRole('button', { name: /Fresh answer/ })).toBeVisible()
})

test('a provider failure is an error that points at pasting a link, never "no results"', async ({ page }) => {
    await openHome(page, { search: () => ({ status: 503, json: { error: { code: 'metadata_provider_busy', message: 'The metadata service is busy right now. Try again in a moment.' } } }) })
    await page.getByPlaceholder(box).fill('dune')
    const alert = page.getByRole('alert')
    await expect(alert).toContainText('busy')
    await expect(alert).toContainText('paste a link')
    await expect(page.getByText(/No results/)).toHaveCount(0)
})

test('searching shows its own loading state', async ({ page }) => {
    await openHome(page, { search: () => ({ json: { results: [dune], hasMore: false }, delay: 800 }) })
    await page.getByPlaceholder(box).fill('dune')
    await expect(page.getByRole('status').filter({ hasText: 'Searching' })).toBeVisible()
    await expect(page.getByRole('list', { name: 'Search results' }).getByRole('button')).toHaveCount(1)
})

test('Movies & TV is a radio group, offered only when the provider is configured, and the choice is remembered', async ({ page }) => {
    await openHome(page)
    const group = page.getByRole('radiogroup', { name: 'Search for' })
    await expect(group.getByRole('radio', { name: 'Movies & TV' })).toBeChecked()
    expect(await page.evaluate(() => localStorage.getItem('requestarr.searchType'))).toBe('movie_tv')
})

test('without a configured provider there is no type selector and typed text points at links', async ({ page }) => {
    const { searches } = await openHome(page, { types: [] })
    await expect(page.getByRole('radiogroup')).toHaveCount(0)
    await page.getByPlaceholder(box).fill('dune')
    await page.getByRole('button', { name: 'Search', exact: true }).click()
    await expect(page.getByRole('alert')).toContainText('paste a link')
    expect(searches).toEqual([])
})

test('Enter searches immediately, and a Viewer can search and open a preview', async ({ page }) => {
    await openHome(page, { role: 'viewer' })
    await page.getByPlaceholder(box).fill('dune')
    await page.getByPlaceholder(box).press('Enter')
    await page.getByRole('list', { name: 'Search results' }).getByRole('button', { name: /^Dune 2021/ }).click()
    await expect(page.getByRole('dialog', { name: /Dune/ })).toBeVisible()
})

test('search results have no automated accessibility violations and keep a 2:3 poster', async ({ page }) => {
    await openHome(page, { search: () => ({ json: { results: [{ ...dune, artworkUrl: 'data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7' }], hasMore: false } }) })
    await page.getByPlaceholder(box).fill('dune')
    const row = page.getByRole('list', { name: 'Search results' }).getByRole('button')
    await expect(row).toHaveCount(1)
    const poster = row.locator('img')
    await expect(poster).toBeVisible()
    const { width, height } = (await poster.boundingBox())!
    expect(Math.abs(height / width - 1.5)).toBeLessThan(0.02)
    const report = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze()
    expect(report.violations).toEqual([])
})

test('a rate-limited search keeps the last results, says to slow down, and recovers by itself', async ({ page }) => {
    let limited = true
    const { searches } = await openHome(page, {
        search: url => {
            if (url.searchParams.get('q') === 'dunes' && limited) { limited = false; return { status: 429, json: { error: { code: 'search_rate_limited', message: 'You are searching too fast. Wait a moment and try again.' } } } }
            return { json: { results: [url.searchParams.get('q') === 'dunes' ? prophecy : dune], hasMore: false } }
        },
    })
    const input = page.getByPlaceholder(box)
    const results = page.getByRole('list', { name: 'Search results' })
    await input.fill('dune')
    await expect(results.getByRole('button')).toHaveCount(1)
    await input.fill('dunes')
    const alert = page.getByRole('alert')
    await expect(alert).toContainText('searching too fast')
    await expect(alert).not.toContainText('No results')
    // The earlier results stay put while the notice shows.
    await expect(results.getByRole('button', { name: /^Dune 2021/ })).toBeVisible()
    // The response carried no Retry-After, so the page waits its default and then searches again on its own.
    await expect(results.getByRole('button', { name: /Prophecy/ })).toBeVisible({ timeout: 10_000 })
    await expect(alert).toHaveCount(0)
    expect(searches.filter(q => q === 'dunes').length).toBeGreaterThanOrEqual(2)
})
