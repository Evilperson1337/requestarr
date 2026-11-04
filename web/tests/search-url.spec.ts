import { expect, test, type Page } from '@playwright/test'

const box = 'Search or paste a link…'
const row = (i: number) => ({ mediaType: 'movie', provider: 'tmdb', providerId: String(1000 + i), ref: `https://www.themoviedb.org/movie/${1000 + i}`, title: `Dune ${i}`, year: 2000 + (i % 20), artworkUrl: '', artworkKind: 'poster' })
const resolved = {
    Item: { ID: '11111111-1111-4111-8111-111111111111', MediaType: 'movie', Provider: 'tmdb', ProviderID: '1000', Title: 'Dune 0', Year: 2000, ArtworkURL: '', ArtworkKind: 'poster', ExternalURL: 'https://www.themoviedb.org/movie/1000' }, Platforms: [],
    Movie: { overview: 'Spice.', genres: [] },
    Preview: { availability: 'unknown', canRequest: true, canSubscribe: false },
}
type Options = { types?: string[]; count?: number; fail?: boolean; delay?: number }

async function open(page: Page, path: string, options: Options = {}) {
    const searches: string[] = []
    const types: string[] = []
    await page.route('**/api/v1/**', async route => {
        const url = new URL(route.request().url())
        const p = url.pathname.replace('/api/v1', '')
        if (p === '/setup/status') return route.fulfill({ json: { complete: true, required: false, wizardComplete: true } })
        if (p === '/auth/me') return route.fulfill({ json: { user: { id: 'user-1', role: 'requester', displayName: 'User' }, csrfToken: 'csrf-token' } })
        if (p === '/search/types') return route.fulfill({ json: { types: options.types ?? ['movie_tv'] } })
        if (p === '/search') {
            searches.push(url.searchParams.get('q') ?? ''); types.push(url.searchParams.get('type') ?? '')
            if (options.delay) await new Promise(resolve => setTimeout(resolve, options.delay))
            if (options.fail) return route.fulfill({ status: 503, json: { error: { code: 'metadata_provider_busy', message: 'The metadata service is busy right now.' } } }).catch(() => undefined)
            const count = options.count ?? 2
            return route.fulfill({ json: { results: Array.from({ length: count }, (_, i) => row(i)), hasMore: false } }).catch(() => undefined)
        }
        if (p === '/resolve') return route.fulfill({ json: resolved })
        return route.fulfill({ json: { items: [], nextCursor: null } })
    })
    await page.goto(path)
    return { searches, types }
}
const results = (page: Page) => page.getByRole('list', { name: 'Search results' })
const live = (page: Page) => page.locator('.searchStatus')
const scrollTotal = (page: Page) => page.evaluate(() => (document.scrollingElement?.scrollTop ?? 0) + (document.querySelector('.appMain')?.scrollTop ?? 0))

test('loading Home with ?q and ?type runs that search and selects that type', async ({ page }) => {
    const { searches, types } = await open(page, '/?q=dune&type=album', { types: ['movie_tv', 'album'] })
    await expect(results(page).getByRole('button')).toHaveCount(2)
    expect(searches[0]).toBe('dune'); expect(types[0]).toBe('album')
    await expect(page.getByPlaceholder(box)).toHaveValue('dune')
    await expect(page.getByRole('radio', { name: 'Albums' })).toBeChecked()
})

for (const type of ['nonsense', 'game']) {
    test(`an unknown or unconfigured type (${type}) in the URL falls back to a valid one`, async ({ page }) => {
        const { types } = await open(page, `/?q=dune&type=${type}`)
        await expect(results(page).getByRole('button')).toHaveCount(2)
        expect(types[0]).toBe('movie_tv')
        await expect(page.getByRole('radio', { name: 'Movies & TV' })).toBeChecked()
        await expect(page.getByRole('alert')).toHaveCount(0)
    })
}

test('typing writes one history entry then replaces it, and Back leaves the search', async ({ page }) => {
    await open(page, '/')
    const before = await page.evaluate(() => history.length)
    const input = page.getByPlaceholder(box)
    await input.pressSequentially('dune', { delay: 40 })
    await expect(results(page).getByRole('button')).toHaveCount(2)
    await input.pressSequentially(' part two', { delay: 40 })
    await expect(live(page)).toHaveText('2 results for dune part two')
    await expect(page).toHaveURL(/\?q=dune\+part\+two&type=movie_tv$/)
    expect(await page.evaluate(() => history.length)).toBe(before + 1)
    await page.goBack()
    await expect(page).toHaveURL(/\/$/)
    await expect(input).toHaveValue('')
    await expect(results(page)).toHaveCount(0)
    await page.goForward()
    await expect(input).toHaveValue('dune part two')
    await expect(results(page).getByRole('button')).toHaveCount(2)
})

test('Back after previewing and navigating away restores query, type and results', async ({ page }) => {
    await open(page, '/')
    await page.getByPlaceholder(box).fill('dune')
    await results(page).getByRole('button', { name: /^Dune 0/ }).click()
    await expect(page.getByRole('dialog')).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(page.getByRole('dialog')).toHaveCount(0)
    await page.getByRole('link', { name: 'Requests', exact: true }).click()
    await expect(page).toHaveURL(/\/requests/)
    await page.goBack()
    await expect(page.getByPlaceholder(box)).toHaveValue('dune')
    await expect(results(page).getByRole('button')).toHaveCount(2)
    await expect(page.getByRole('radio', { name: 'Movies & TV' })).toBeChecked()
})

test('a pasted link is never written to the URL as q', async ({ page }) => {
    await open(page, '/')
    const input = page.getByPlaceholder(box)
    await input.fill('https://www.themoviedb.org/movie/1000')
    await input.press('Enter')
    await expect(page.getByRole('dialog')).toBeVisible()
    expect(new URL(page.url()).searchParams.has('q')).toBe(false)
})

test('clearing the box removes q from the URL', async ({ page }) => {
    await open(page, '/')
    const input = page.getByPlaceholder(box)
    await input.fill('dune')
    await expect(page).toHaveURL(/q=dune/)
    await input.fill('')
    await expect(page).not.toHaveURL(/q=/)
    await expect(results(page)).toHaveCount(0)
})

for (const close of ['Escape', 'Close preview']) {
    test(`closing the modal with ${close} returns focus to the row and keeps the scroll position`, async ({ page }) => {
        await open(page, '/?q=dune', { count: 40 })
        const target = results(page).getByRole('button', { name: /^Dune 30 / })
        await target.scrollIntoViewIfNeeded()
        const scrolled = await scrollTotal(page)
        expect(scrolled).toBeGreaterThan(0)
        await target.click()
        const dialog = page.getByRole('dialog')
        await expect(dialog).toBeVisible()
        if (close === 'Escape') await page.keyboard.press('Escape')
        else await dialog.getByRole('button', { name: close, exact: true }).click()
        await expect(dialog).toHaveCount(0)
        await expect(target).toBeFocused()
        expect(await scrollTotal(page)).toBe(scrolled)
    })
}

test('keyboard only: radio arrows, results in order, Enter opens, Escape returns focus', async ({ page }) => {
    await open(page, '/', { types: ['movie_tv', 'album'] })
    await page.getByPlaceholder(box).focus()
    await page.keyboard.type('dune')
    await expect(results(page).getByRole('button')).toHaveCount(2)
    await page.keyboard.press('Tab') // Search button
    await page.keyboard.press('Tab') // radio group: one stop, on the checked radio
    await expect(page.getByRole('radio', { name: 'Movies & TV' })).toBeFocused()
    await page.keyboard.press('ArrowDown')
    await expect(page.getByRole('radio', { name: 'Albums' })).toBeChecked()
    await page.keyboard.press('ArrowUp')
    await expect(page.getByRole('radio', { name: 'Movies & TV' })).toBeChecked()
    await expect(results(page).getByRole('button')).toHaveCount(2) // a typeahead type searches again
    await page.keyboard.press('Tab')
    await expect(results(page).getByRole('button').nth(0)).toBeFocused()
    await page.keyboard.press('Tab')
    await expect(results(page).getByRole('button').nth(1)).toBeFocused()
    await page.keyboard.press('Enter')
    await expect(page.getByRole('dialog')).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(page.getByRole('dialog')).toHaveCount(0)
    await expect(results(page).getByRole('button').nth(1)).toBeFocused()
})

test('the live region reports Searching, the result count and an empty search', async ({ page }) => {
    await open(page, '/?q=dune', { delay: 500 })
    await expect(live(page)).toHaveText('Searching…')
    await expect(live(page)).toHaveText('2 results for dune')
})

test('the live region reports an empty search', async ({ page }) => {
    await open(page, '/?q=zzz', { count: 0 })
    await expect(live(page)).toContainText('No results for zzz')
})

test('the live region reports Search unavailable, never "no results", on a provider failure', async ({ page }) => {
    await open(page, '/?q=dune', { fail: true })
    await expect(live(page)).toHaveText('Search unavailable')
    await expect(page.getByRole('alert')).toContainText('paste a link')
})
