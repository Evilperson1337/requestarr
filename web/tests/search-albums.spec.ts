import AxeBuilder from '@axe-core/playwright'
import { expect, test, type Page } from '@playwright/test'
import { letTimersRun } from './fixtures'

const box = 'Search or paste a link…'
const pixel = 'data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7'
const moon = { mediaType: 'album', provider: 'musicbrainz', providerId: 'f5093c06-23e3-404f-aeaa-40f72885ee3a', ref: 'https://musicbrainz.org/release-group/f5093c06-23e3-404f-aeaa-40f72885ee3a', title: 'The Dark Side of the Moon', year: 1973, subtitle: 'Album · Pink Floyd', artworkUrl: pixel, artworkKind: 'square' }
const single = { mediaType: 'album', provider: 'musicbrainz', providerId: '2b8c3a77-5d1e-4e0a-8f6e-6a1f0c9d2b02', ref: 'https://musicbrainz.org/release-group/2b8c3a77-5d1e-4e0a-8f6e-6a1f0c9d2b02', title: 'Dark Side Single', year: 2001, subtitle: 'Single · Gamma', artworkUrl: '', artworkKind: 'square' }
const resolved = {
    Item: { ID: '22222222-2222-4222-8222-222222222222', MediaType: 'album', Provider: 'musicbrainz', ProviderID: moon.providerId, Title: 'Pink Floyd — The Dark Side of the Moon', Year: 1973, ArtworkURL: '', ArtworkKind: 'square', ExternalURL: moon.ref }, Platforms: [],
    Music: { Artists: ['Pink Floyd'], PrimaryType: 'Album', Genres: [], Tracks: [] },
}

type Reply = { status?: number; json: unknown; delay?: number }

async function openHome(page: Page, search?: (url: URL) => Reply) {
    const searches: URL[] = []
    const resolves: string[] = []
    await page.route('**/api/v1/**', async route => {
        const url = new URL(route.request().url())
        const path = url.pathname.replace('/api/v1', '')
        if (path === '/setup/status') return route.fulfill({ json: { complete: true, required: false, wizardComplete: true } })
        if (path === '/auth/me') return route.fulfill({ json: { user: { id: 'user-1', role: 'requester', displayName: 'User' }, csrfToken: 'csrf-token' } })
        if (path === '/search/types') return route.fulfill({ json: { types: ['album', 'movie_tv'] } })
        if (path === '/search') {
            searches.push(url)
            const reply = search?.(url) ?? { json: { results: url.searchParams.get('singles') === 'true' ? [moon, single] : [moon], hasMore: false } }
            if (reply.delay) await new Promise(resolve => setTimeout(resolve, reply.delay))
            return route.fulfill({ status: reply.status, json: reply.json }).catch(() => undefined)
        }
        if (path === '/resolve') { resolves.push(route.request().postDataJSON().url); return route.fulfill({ json: resolved }) }
        return route.fulfill({ json: { items: [], nextCursor: null } })
    })
    await page.clock.install()
    await page.goto('/')
    await page.getByRole('radio', { name: 'Albums' }).check()
    return { searches, resolves }
}

test('Albums is offered as a type and does not search while typing, only on Enter', async ({ page }) => {
    const { searches } = await openHome(page)
    await page.getByPlaceholder(box).fill('dark side')
    await letTimersRun(page, 900)
    expect(searches).toEqual([])
    await page.getByPlaceholder(box).press('Enter')
    const results = page.getByRole('list', { name: 'Search results' })
    await expect(results.getByRole('button')).toHaveCount(1)
    expect(searches).toHaveLength(1)
    expect(searches[0].searchParams.get('type')).toBe('album')
    expect(searches[0].searchParams.get('q')).toBe('dark side')
    expect(searches[0].searchParams.get('singles')).toBeNull()
    // Type, artist and year are in the row; the live region announces the count.
    await expect(results.getByRole('button', { name: /The Dark Side of the Moon.*1973.*Album · Pink Floyd/ })).toBeVisible()
    await expect(page.getByRole('status')).toContainText('1 result for dark side')
    await expect(page.getByPlaceholder(box)).toBeFocused()
})

test('the Search button submits, and typing afterwards neither searches nor clears the answer', async ({ page }) => {
    const { searches } = await openHome(page)
    await page.getByPlaceholder(box).fill('dark side')
    await page.getByRole('button', { name: 'Search', exact: true }).click()
    const results = page.getByRole('list', { name: 'Search results' })
    await expect(results.getByRole('button')).toHaveCount(1)
    await page.getByPlaceholder(box).fill('dark side of the')
    await letTimersRun(page, 900)
    expect(searches).toHaveLength(1)
    await expect(results.getByRole('button')).toHaveCount(1)
})

test('"Include singles & EPs" is off by default, belongs to Albums only, and searches again when changed', async ({ page }) => {
    const { searches } = await openHome(page)
    const toggle = page.getByRole('checkbox', { name: 'Include singles & EPs' })
    await expect(toggle).not.toBeChecked()
    await page.getByPlaceholder(box).fill('dark side')
    await page.getByPlaceholder(box).press('Enter')
    const results = page.getByRole('list', { name: 'Search results' })
    await expect(results.getByRole('button')).toHaveCount(1)
    await toggle.check()
    await expect(results.getByRole('button')).toHaveCount(2)
    expect(searches.at(-1)?.searchParams.get('singles')).toBe('true')
    await expect(results.getByRole('button', { name: /Dark Side Single.*2001.*Single · Gamma/ })).toBeVisible()
    await page.getByRole('radio', { name: 'Movies & TV' }).check()
    await expect(toggle).toHaveCount(0)
})

test('a busy MusicBrainz is an error that points at pasting a link, never "no results"', async ({ page }) => {
    await openHome(page, () => ({ status: 503, json: { error: { code: 'metadata_provider_busy', message: 'The metadata service is busy right now. Try again in a moment.' } } }))
    await page.getByPlaceholder(box).fill('dark side')
    await page.getByPlaceholder(box).press('Enter')
    await expect(page.getByRole('alert')).toContainText('busy')
    await expect(page.getByRole('alert')).toContainText('paste a link')
    await expect(page.getByText(/No results/)).toHaveCount(0)
})

test('picking an album result resolves its release-group link', async ({ page }) => {
    const { resolves } = await openHome(page)
    await page.getByPlaceholder(box).fill('dark side')
    await page.getByPlaceholder(box).press('Enter')
    await page.getByRole('list', { name: 'Search results' }).getByRole('button', { name: /The Dark Side of the Moon/ }).click()
    await expect(page.getByRole('dialog')).toBeVisible()
    expect(resolves).toEqual([moon.ref])
})

test('album rows keep square artwork and have no accessibility violations', async ({ page }) => {
    await openHome(page)
    await page.getByPlaceholder(box).fill('dark side')
    await page.getByPlaceholder(box).press('Enter')
    const cover = page.locator('.searchRow img').first()
    await expect(cover).toBeVisible()
    const box_ = await cover.boundingBox()
    expect(Math.abs((box_?.width ?? 0) - (box_?.height ?? 1))).toBeLessThan(1)
    const scan = await new AxeBuilder({ page }).analyze()
    expect(scan.violations).toEqual([])
})

test('a cover that fails to load leaves a plain placeholder, not a broken image', async ({ page }) => {
    await openHome(page, () => ({ json: { results: [{ ...moon, artworkUrl: 'http://127.0.0.1:9/missing.jpg' }], hasMore: false } }))
    await page.getByPlaceholder(box).fill('dark side')
    await page.getByPlaceholder(box).press('Enter')
    await expect(page.getByRole('list', { name: 'Search results' }).getByRole('button')).toHaveCount(1)
    await expect(page.locator('.searchRow img')).toHaveCount(0)
})
