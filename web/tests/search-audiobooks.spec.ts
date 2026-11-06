import AxeBuilder from '@axe-core/playwright'
import { expect, test, type Page } from '@playwright/test'
import { letTimersRun } from './fixtures'

const box = 'Search or paste a link…'
const pixel = 'data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7'
const phm = { mediaType: 'audiobook', provider: 'audible', providerId: 'B08G9PRS1K', ref: 'https://www.audible.com/pd/B08G9PRS1K', title: 'Project Hail Mary', year: 2021, subtitle: 'Andy Weir · Narrated by Ray Porter · 16h 10m · Unabridged · Series / multi-part', artworkUrl: pixel, artworkKind: 'square', local: { availability: 'available' } }
const dramatized = { mediaType: 'audiobook', provider: 'audible', providerId: 'B0BYKC2P4M', ref: 'https://www.audible.com/pd/B0BYKC2P4M', title: 'Project Hail Mary', year: 2023, subtitle: 'Andy Weir · Narrated by Full Cast · 14h 0m · Abridged', artworkUrl: '', artworkKind: 'square', local: { availability: 'unknown', requestStatus: 'requested' } }

type Reply = { status?: number; json: unknown }

async function openHome(page: Page, types: string[], search?: (url: URL) => Reply) {
    const searches: URL[] = []
    const resolves: string[] = []
    await page.route('**/api/v1/**', async route => {
        const url = new URL(route.request().url())
        const path = url.pathname.replace('/api/v1', '')
        if (path === '/auth/me') return route.fulfill({ json: { user: { id: 'user-1', role: 'requester', displayName: 'User' }, csrfToken: 'csrf-token' } })
        if (path === '/search/types') return route.fulfill({ json: { types } })
        if (path === '/search') {
            searches.push(url)
            const reply = search?.(url) ?? { json: { results: [phm, dramatized], hasMore: false } }
            return route.fulfill({ status: reply.status, json: reply.json })
        }
        if (path === '/resolve') { resolves.push(route.request().postDataJSON().url); return route.fulfill({ status: 500, json: {} }) }
        return route.fulfill({ json: { items: [], nextCursor: null } })
    })
    await page.clock.install()
    await page.goto('/')
    return { searches, resolves }
}

test('Audiobooks appears only when the server offers it, with no region control', async ({ page }) => {
    await openHome(page, ['movie_tv'])
    await expect(page.getByRole('radio', { name: 'Movies & TV' })).toBeVisible()
    await expect(page.getByRole('radio', { name: 'Audiobooks' })).toHaveCount(0)
})

test('audiobook search waits for Enter, shows narrator, runtime, format and editions as separate rows', async ({ page }) => {
    const { searches } = await openHome(page, ['movie_tv', 'audiobook'])
    await expect(page.getByRole('combobox', { name: /region|marketplace/i })).toHaveCount(0)
    await page.getByRole('radio', { name: 'Audiobooks' }).check()
    await expect(page.locator('#searchHint')).toContainText('own request')
    await page.getByPlaceholder(box).fill('project hail mary')
    await letTimersRun(page, 900)
    expect(searches).toEqual([])
    await page.getByPlaceholder(box).press('Enter')
    const rows = page.getByRole('list', { name: 'Search results' }).getByRole('button')
    await expect(rows).toHaveCount(2)
    expect(searches[0].searchParams.get('type')).toBe('audiobook')
    await expect(rows.nth(0)).toContainText('Narrated by Ray Porter · 16h 10m · Unabridged · Series / multi-part')
    await expect(rows.nth(0)).toContainText('In library')
    await expect(rows.nth(1)).toContainText('Abridged')
    await expect(rows.nth(1)).toContainText('Requested')
    const scan = await new AxeBuilder({ page }).analyze()
    expect(scan.violations).toEqual([])
})

test('a failing Audible search is an error, never "no results"', async ({ page }) => {
    await openHome(page, ['audiobook'], () => ({ status: 503, json: { error: { code: 'metadata_provider_busy', message: 'The metadata service is busy right now. Try again in a moment.' } } }))
    await page.getByRole('radio', { name: 'Audiobooks' }).check()
    await page.getByPlaceholder(box).fill('project hail mary')
    await page.getByPlaceholder(box).press('Enter')
    await expect(page.getByRole('alert')).toContainText('paste a link')
    await expect(page.getByText(/No results/)).toHaveCount(0)
})

test('picking an audiobook row resolves its Audible product link', async ({ page }) => {
    const { resolves } = await openHome(page, ['audiobook'])
    await page.getByRole('radio', { name: 'Audiobooks' }).check()
    await page.getByPlaceholder(box).fill('project hail mary')
    await page.getByPlaceholder(box).press('Enter')
    await page.getByRole('list', { name: 'Search results' }).getByRole('button').first().click()
    await expect.poll(() => resolves).toEqual([phm.ref])
})
