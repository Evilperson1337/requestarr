import { expect, test, type Page } from '@playwright/test'
import { capabilitiesFor } from './capabilities'

const row = (id: string, title: string, mediaType: string, status: string, over: object = {}) => ({
    ID: id, CatalogItemID: 'c' + id, Title: title, MediaType: mediaType, ArtworkURL: '', ArtworkKind: 'poster', Status: status, Version: 1,
    RequestedBy: 'user-1', Note: '', availability: 'unknown', Platforms: { any: true }, CreatedAt: '2026-10-28T12:00:00Z', subscribed: true, subscriptionSource: 'requester', ...over,
})

type Reply = { status?: number; json: unknown }
const noLibrary = (): Reply => ({ json: { items: [] } })

async function openHome(page: Page, requests: (url: URL) => Reply, library: (url: URL) => Reply = noLibrary, other: (path: string) => Reply | undefined = () => undefined) {
    const calls: string[] = []
    await page.route('**/api/v1/**', route => {
        const url = new URL(route.request().url())
        const path = url.pathname.replace('/api/v1', '')
        if (path === '/auth/me') return route.fulfill({ json: { user: { id: 'user-1', role: 'requester', displayName: 'User' }, capabilities: capabilitiesFor('requester'), csrfToken: 'csrf-token' } })
        if (path === '/requests') { calls.push(url.search); return route.fulfill(requests(url)) }
        if (path === '/library/recent') return route.fulfill(library(url))
        const extra = other(path)
        if (extra) return route.fulfill(extra)
        return route.fulfill({ status: 404, json: { error: { code: 'not_found', message: 'unmocked' } } })
    })
    await page.goto('/')
    return calls
}

test('Home lists the newest requests the user follows, as many as fit one row, with status and library state', async ({ page }) => {
    const calls = await openHome(page, () => ({ json: { items: [row('1', 'Arrival', 'movie', 'requested'), row('2', 'Severance', 'tv', 'available', { availability: 'available' }), row('3', 'Kid A', 'album', 'approved', { ArtworkURL: '/art/kid-a.png', ArtworkKind: 'square' })], nextCursor: null } }))
    const section = page.getByRole('region', { name: 'Your recent requests' })
    await expect(section.getByRole('link', { name: 'Arrival' })).toHaveAttribute('href', '/requests?requestId=1')
    await expect(section.getByRole('listitem')).toHaveCount(3)
    await expect(section.getByRole('listitem').filter({ hasText: 'Severance' })).toContainText('available')
    await expect(section.getByRole('img', { name: 'Artwork for Kid A' })).toBeVisible()
    await expect(section.getByRole('link', { name: 'Open the full queue' })).toHaveAttribute('href', '/requests?mine=true')
    // The dev server runs effects twice; the query is what matters.
    expect([...new Set(calls)].filter(call => call.includes('mine'))).toEqual(['?mine=true&limit=12'])
})

test('Home says so when nothing has been requested yet', async ({ page }) => {
    await openHome(page, () => ({ json: { items: [], nextCursor: null } }))
    await expect(page.getByText('Nothing yet. Paste a link above to make your first request.')).toBeVisible()
})

test('a failing list never blocks requesting', async ({ page }) => {
    await openHome(page, () => ({ status: 500, json: { error: { code: 'requests_list_failed', message: 'could not list requests' } } }))
    await expect(page.getByText('Your recent requests could not be loaded.')).toBeVisible()
    await expect(page.getByPlaceholder('Search or paste a link…')).toBeEnabled()
    await expect(page.getByRole('button', { name: 'Preview', exact: true })).toBeEnabled()
})

test('Home adds a Recently available row from shared requests, and omits it when empty', async ({ page }) => {
    const calls = await openHome(page, url => ({ json: { items: url.searchParams.get('status') === 'available' ? [row('9', 'Arrival', 'movie', 'available')] : [], nextCursor: null } }))
    const available = page.getByRole('region', { name: 'Recently available' })
    await expect(available.getByRole('link', { name: /Arrival/ })).toHaveAttribute('href', '/requests?requestId=9')
    await expect(available.getByRole('link', { name: 'All available' })).toHaveAttribute('href', '/requests?status=available')
    expect([...new Set(calls)]).toContain('?status=available&limit=12')
})

test('Home leaves the Recently available row out when there is nothing to show', async ({ page }) => {
    await openHome(page, () => ({ json: { items: [], nextCursor: null } }))
    await expect(page.getByText('Nothing yet. Paste a link above to make your first request.')).toBeVisible()
    await expect(page.getByRole('region', { name: 'Recently available' })).toHaveCount(0)
})

test('Home shows what the libraries recently gained as one shelf per cover shape', async ({ page }) => {
    const calls: string[] = []
    await openHome(page, () => ({ json: { items: [], nextCursor: null } }), url => {
        calls.push(url.search)
        const types = url.searchParams.get('types')
        return { json: { items: types === 'movie,tv'
            ? [{ id: 'a', title: 'Arrival', year: 2016, mediaType: 'movie', source: 'Plex', addedAt: '2026-11-01T12:00:00Z', artworkUrl: '/api/v1/library/items/a/artwork' }]
            : types === 'game'
                ? [{ id: 'g', title: 'Hades II', mediaType: 'game', detail: 'Windows', source: 'RomM', addedAt: '2026-10-29T12:00:00Z', artworkUrl: '/api/v1/library/items/g/artwork' }]
                : [{ id: 'b', title: 'Kid A', year: 2000, mediaType: 'album', source: 'Plex', addedAt: '2026-10-30T12:00:00Z' }] } }
    })
    const shows = page.getByRole('region', { name: 'New movies and shows in your libraries' })
    const music = page.getByRole('region', { name: 'New music in your libraries' })
    await expect(shows.getByRole('img', { name: 'Cover for Arrival' })).toHaveAttribute('src', '/api/v1/library/items/a/artwork')
    await expect(shows.getByRole('listitem')).toContainText('Movie · 2016 · Plex')
    await expect(music.getByRole('listitem')).toContainText('Album · 2000 · Plex')
    const games = page.getByRole('region', { name: 'New games in your libraries' })
    await expect(games.getByRole('listitem')).toContainText('Game · Windows · RomM')
    await expect(games.locator('.railCover.game')).toHaveCount(1)
    // Square art never shares a row with posters, and nothing in either shelf is a link.
    await expect(shows.locator('.railCover.square')).toHaveCount(0)
    await expect(music.locator('.railCover.square')).toHaveCount(1)
    await expect(page.getByRole('region', { name: /in your libraries/ }).getByRole('link')).toHaveCount(0)
    // A library shelf pages with arrows (and swipes), so it asks for several rows' worth, not one.
    expect([...new Set(calls)].sort()).toEqual(['?types=album&limit=48', '?types=audiobook&limit=48', '?types=game&limit=48', '?types=movie,tv&limit=48'])
})

test('Home leaves the library shelves out when libraries have nothing to show', async ({ page }) => {
    await openHome(page, () => ({ json: { items: [], nextCursor: null } }))
    await expect(page.getByText('Nothing yet. Paste a link above to make your first request.')).toBeVisible()
    await expect(page.getByRole('region', { name: /in your libraries/ })).toHaveCount(0)
})

const arrivalRecent = { id: 'a', title: 'Arrival', year: 2016, mediaType: 'movie', source: 'Plex', addedAt: '2026-11-01T12:00:00Z' }
const arrivalFull = { ...arrivalRecent, sourceKind: 'plex', openIssues: 0, link: 'https://plex.example/web#!/x', issueKinds: [{ kind: 'video', label: 'Video' }], details: { synopsis: 'A linguist is recruited to talk to visitors.', rating: 7.6 } }
const recentMovies = (url: URL): Reply => ({ json: { items: url.searchParams.get('types') === 'movie,tv' ? [arrivalRecent] : [] } })
const noRequests = (): Reply => ({ json: { items: [], nextCursor: null } })

test('a cover under "New … in your libraries" opens that item, as it does in the Library', async ({ page }) => {
    const fetched: string[] = []
    await openHome(page, noRequests, recentMovies, path => { if (path === '/library/items/a') { fetched.push(path); return { json: arrivalFull } } })
    const shelf = page.getByRole('region', { name: 'New movies and shows in your libraries' })
    const cover = shelf.getByRole('button', { name: /Arrival/ })
    await cover.click()
    const dialog = page.getByRole('dialog')
    await expect(dialog.getByRole('heading', { name: 'Arrival (2016)' })).toBeVisible()
    await expect(dialog.getByRole('region', { name: 'Synopsis' })).toContainText('A linguist is recruited')
    await expect(dialog.getByRole('link', { name: /Open in Plex/ })).toHaveAttribute('href', 'https://plex.example/web#!/x')
    await expect(dialog.getByRole('button', { name: 'Report an issue' })).toBeVisible()
    expect(fetched.length).toBeGreaterThan(0)
    await page.keyboard.press('Escape')
    await expect(dialog).toBeHidden()
    await expect(cover).toBeFocused()
})

test('an item that cannot be opened says so and the shelf keeps working', async ({ page }) => {
    await openHome(page, noRequests, recentMovies, path => path === '/library/items/a' ? { status: 500, json: { error: { code: 'library_browse_failed', message: 'could not load the library item' } } } : undefined)
    const shelf = page.getByRole('region', { name: 'New movies and shows in your libraries' })
    await shelf.getByRole('button', { name: /Arrival/ }).click()
    await expect(page.getByRole('alert').filter({ hasText: 'could not be opened' })).toBeVisible()
    await expect(page.getByRole('dialog')).toHaveCount(0)
    await expect(shelf.getByRole('button', { name: /Arrival/ })).toBeEnabled()
})
