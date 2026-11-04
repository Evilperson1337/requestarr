import AxeBuilder from '@axe-core/playwright'
import { expect, test, type Page } from '@playwright/test'

const box = 'Search or paste a link…'
const base = { provider: 'tmdb', artworkUrl: '', artworkKind: 'poster' }
const requested = { ...base, mediaType: 'tv', providerId: '1', ref: 'https://www.themoviedb.org/tv/1', title: 'Requested Show', year: 2021, local: { catalogId: '11111111-1111-4111-8111-111111111111', requestStatus: 'approved', availability: 'unknown' } }
const held = { ...base, mediaType: 'movie', providerId: '2', ref: 'https://www.themoviedb.org/movie/2', title: 'Held Movie', year: 2020, local: { availability: 'available' } }
const unseen = { ...base, mediaType: 'movie', providerId: '3', ref: 'https://www.themoviedb.org/movie/3', title: 'Unseen Movie', year: 2019 }
const resolved = {
    Item: { ID: '11111111-1111-4111-8111-111111111111', MediaType: 'tv', Provider: 'tmdb', ProviderID: '1', Title: 'Requested Show', Year: 2021, ArtworkURL: '', ArtworkKind: 'poster', ExternalURL: 'https://www.themoviedb.org/tv/1' }, Platforms: [],
    TV: { overview: 'A series.', seasons: 2, episodes: 4, seasonList: [{ number: 1, name: 'Season 1', episodeCount: 2, episodes: [{ number: 1, name: 'Pilot' }, { number: 2, name: 'Second' }] }, { number: 2, name: 'Season 2', episodeCount: 2, episodes: [{ number: 1, name: 'Return' }, { number: 2, name: 'Finale' }] }] },
    Preview: { availability: 'unknown', canRequest: true, canSubscribe: true, canExtend: true, request: { id: '22222222-2222-4222-8222-222222222222', status: 'approved', subscribed: false, selection: { seasons: [1] } } },
}

async function openHome(page: Page) {
    await page.route('**/api/v1/**', async route => {
        const path = new URL(route.request().url()).pathname.replace('/api/v1', '')
        if (path === '/setup/status') return route.fulfill({ json: { complete: true, required: false, wizardComplete: true } })
        if (path === '/auth/me') return route.fulfill({ json: { user: { id: 'user-1', role: 'requester', displayName: 'User' }, csrfToken: 'csrf-token' } })
        if (path === '/search/types') return route.fulfill({ json: { types: ['movie_tv'] } })
        if (path === '/search') return route.fulfill({ json: { results: [requested, held, unseen], hasMore: false } })
        if (path === '/resolve') return route.fulfill({ json: resolved })
        return route.fulfill({ json: { items: [], nextCursor: null } })
    })
    await page.goto('/')
    await page.getByPlaceholder(box).fill('movie')
    await expect(page.getByRole('list', { name: 'Search results' }).getByRole('button')).toHaveCount(3)
}

test('requested and in-library results say so in words, and nothing is hidden', async ({ page }) => {
    await openHome(page)
    const rows = page.getByRole('list', { name: 'Search results' }).getByRole('button')
    await expect(rows.filter({ hasText: 'Requested Show' })).toContainText('Requested · Approved')
    await expect(rows.filter({ hasText: 'Held Movie' })).toContainText('In library')
    // Never-seen means no local state and no badge at all.
    await expect(rows.filter({ hasText: 'Unseen Movie' }).locator('.searchBadge')).toHaveCount(0)
    await expect(rows.filter({ hasText: 'Held Movie' })).not.toContainText('Requested')
})

test('badges are readable without colour: text and an icon, with no colour-only signal, and pass axe', async ({ page }) => {
    await openHome(page)
    const badges = page.locator('.searchBadge')
    await expect(badges).toHaveCount(2)
    for (const badge of await badges.all()) {
        expect(((await badge.textContent()) ?? '').trim().length).toBeGreaterThan(4)
        await expect(badge.locator('[aria-hidden="true"]')).toHaveCount(1) // the icon; the words carry the meaning
    }
    // Strip every colour: the words and the icon are all that is left to tell them apart.
    await page.addStyleTag({ content: '*{color:#000 !important;background:#fff !important;border-color:#000 !important}' })
    await expect(page.getByText('In library')).toBeVisible()
    await expect(page.getByText('Requested · Approved')).toBeVisible()
    const report = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze()
    expect(report.violations).toEqual([])
})

test('clicking an already-requested result opens the usual modal with Notify Me, View request and Add to request', async ({ page }) => {
    await openHome(page)
    await page.getByRole('list', { name: 'Search results' }).getByRole('button', { name: /Requested Show/ }).click()
    const dialog = page.getByRole('dialog', { name: /Requested Show/ })
    await expect(dialog).toBeVisible()
    await expect(dialog.getByRole('button', { name: 'Notify Me' })).toBeVisible()
    await expect(dialog.getByRole('link', { name: 'View request' })).toBeVisible()
    await expect(dialog.getByRole('button', { name: 'Add to request' })).toBeVisible()
})
