import AxeBuilder from '@axe-core/playwright'
import { expect, test, type Page } from '@playwright/test'
import { letTimersRun } from './fixtures'

const box = 'Search or paste a link…'
const id = 'b1a9c0e9-d987-4042-ae91-78d6a3267d69'
const money = { mediaType: 'song', provider: 'musicbrainz', providerId: id, ref: `https://musicbrainz.org/recording/${id}`, title: 'Money', year: 1973, subtitle: 'Pink Floyd · 6:22 · The Dark Side of the Moon', artworkUrl: '', artworkKind: 'square', local: { availability: 'available' } }
const other = { ...money, providerId: '2b8c3a77-5d1e-4e0a-8f6e-6a1f0c9d2b02', ref: 'https://musicbrainz.org/recording/2b8c3a77-5d1e-4e0a-8f6e-6a1f0c9d2b02', title: 'Money (live)', subtitle: 'Pink Floyd · 8:00 · Delicate Sound of Thunder', local: { catalogId: '11111111-1111-4111-8111-111111111111', requestStatus: 'requested', availability: 'unknown' } }
const resolved = {
    Item: { ID: '22222222-2222-4222-8222-222222222222', MediaType: 'song', Provider: 'musicbrainz', ProviderID: id, Title: 'Pink Floyd — Money', Year: 1973, ArtworkURL: '', ArtworkKind: 'square', ExternalURL: money.ref }, Platforms: [],
    Music: { Artists: ['Pink Floyd'], Genres: [], Tracks: [] },
}

async function openHome(page: Page) {
    const searches: URL[] = []
    const resolves: string[] = []
    await page.route('**/api/v1/**', async route => {
        const url = new URL(route.request().url())
        const path = url.pathname.replace('/api/v1', '')
        if (path === '/setup/status') return route.fulfill({ json: { complete: true, required: false } })
        if (path === '/auth/me') return route.fulfill({ json: { user: { id: 'user-1', role: 'requester', displayName: 'User' }, csrfToken: 'csrf-token' } })
        if (path === '/search/types') return route.fulfill({ json: { types: ['album', 'song', 'movie_tv'] } })
        if (path === '/search') { searches.push(url); return route.fulfill({ json: { results: [money, other], hasMore: false } }) }
        if (path === '/resolve') { resolves.push(route.request().postDataJSON().url); return route.fulfill({ json: resolved }) }
        return route.fulfill({ json: { items: [], nextCursor: null } })
    })
    await page.clock.install()
    await page.goto('/')
    await page.getByRole('radio', { name: 'Songs' }).check()
    return { searches, resolves }
}

test('Songs searches only on Enter or the button, never while typing, and has no singles switch', async ({ page }) => {
    const { searches } = await openHome(page)
    await expect(page.getByRole('checkbox', { name: 'Include singles & EPs' })).toHaveCount(0)
    await page.getByPlaceholder(box).fill('money floyd')
    await letTimersRun(page, 900)
    expect(searches).toEqual([])
    await expect(page.locator('#searchHint')).toContainText('Songs are looked up when you press Enter')
    await page.getByPlaceholder(box).press('Enter')
    const results = page.getByRole('list', { name: 'Search results' })
    await expect(results.getByRole('button')).toHaveCount(2)
    expect(searches).toHaveLength(1)
    expect(searches[0].searchParams.get('type')).toBe('song')
    expect(searches[0].searchParams.get('singles')).toBeNull()
    // Artist, length and first release are in the row text.
    await expect(results.getByRole('button', { name: /Money.*1973.*Pink Floyd · 6:22 · The Dark Side of the Moon/ })).toBeVisible()
})

test('song rows say In library and Requested in words, and pass axe', async ({ page }) => {
    await openHome(page)
    await page.getByPlaceholder(box).fill('money')
    await page.getByPlaceholder(box).press('Enter')
    const rows = page.getByRole('list', { name: 'Search results' }).getByRole('button')
    await expect(rows.filter({ hasText: 'Pink Floyd · 6:22' })).toContainText('In library')
    await expect(rows.filter({ hasText: 'Money (live)' })).toContainText('Requested')
    const scan = await new AxeBuilder({ page }).analyze()
    expect(scan.violations).toEqual([])
})

test('the Songs hint tells people to add the artist, with a song example, and is tied to the input', async ({ page }) => {
    await openHome(page)
    await expect(page.locator('#searchHint')).toHaveText('Songs are looked up when you press Enter or Search, not as you type. Add the artist for better matches, for example “queen bohemian rhapsody”.')
    await expect(page.getByPlaceholder(box)).toHaveAttribute('aria-describedby', 'searchHint')
})

test('picking a song result resolves its recording link', async ({ page }) => {
    const { resolves } = await openHome(page)
    await page.getByPlaceholder(box).fill('money')
    await page.getByPlaceholder(box).press('Enter')
    await page.getByRole('list', { name: 'Search results' }).getByRole('button', { name: /Pink Floyd · 6:22/ }).click()
    await expect(page.getByRole('dialog')).toBeVisible()
    expect(resolves).toEqual([money.ref])
})
