import { expect, test, type Page } from '@playwright/test'
import AxeBuilder from '@axe-core/playwright'

const album = {
    Item: { ID: '11111111-1111-4111-8111-111111111111', MediaType: 'album', Provider: 'musicbrainz', Title: 'Fixture album', Year: 2000, ArtworkURL: '', ArtworkKind: 'square', ExternalURL: 'https://musicbrainz.org/release-group/x' }, Platforms: [],
    Music: { primaryType: 'Album', artists: ['Someone'], tracks: [{ id: 't1', position: 1, title: 'First', lengthSeconds: 251 }, { id: 't2', position: 2, title: 'Second', lengthSeconds: 65 }, { id: 't3', position: 3, title: 'Third' }] },
    Preview: { availability: 'unknown', canRequest: true, canSubscribe: false, canExtend: false },
}
const series = {
    Item: { ID: '22222222-2222-4222-8222-222222222222', MediaType: 'tv', Provider: 'tmdb', Title: 'Fixture series', Year: 2021, ArtworkURL: '', ArtworkKind: 'poster', ExternalURL: 'https://www.themoviedb.org/tv/1' }, Platforms: [],
    TV: { overview: 'A series.', seasons: 2, episodes: 4, seasonList: [
        { number: 1, name: 'Season 1', episodeCount: 2, episodes: [{ number: 1, name: 'Pilot' }, { number: 2, name: 'Second' }] },
        { number: 2, name: 'Season 2', episodeCount: 2, episodes: [{ number: 1, name: 'Return' }, { number: 2, name: 'Finale' }] },
    ] },
    Preview: { availability: 'unknown', canRequest: true, canSubscribe: false, canExtend: false },
}

async function open(page: Page, resolved: unknown, link: string, posts: { body?: any }, preview?: unknown) {
    await page.route('**/api/v1/**', route => {
        const request = route.request(), path = new URL(request.url()).pathname
        if (path.endsWith('/auth/me')) return route.fulfill({ json: { user: { id: 'u1', role: 'requester', displayName: 'U' }, csrfToken: 'c' } })
        if (path.endsWith('/resolve')) return route.fulfill({ json: resolved })
        if (path.endsWith('/preview')) return route.fulfill({ json: preview ?? (resolved as any).Preview })
        if (path.endsWith('/requests') && request.method() === 'POST') { posts.body = request.postDataJSON(); return route.fulfill({ status: 201, json: { Created: true, Request: { ID: 'r1', Status: 'requested' } } }) }
        return route.fulfill({ json: { items: [], nextCursor: null } })
    })
    await page.goto('/')
    await page.getByPlaceholder('Search or paste a link…').fill(link)
    await page.getByRole('button', { name: 'Preview', exact: true }).click()
    return page.getByRole('dialog')
}

test('an album can be requested whole or by chosen tracks', async ({ page }) => {
    const posts: { body?: any } = {}
    const dialog = await open(page, album, 'https://musicbrainz.org/release-group/x', posts)
    await expect(dialog.getByRole('radio', { name: 'Whole album' })).toBeChecked()
    await expect(dialog.getByRole('list', { name: 'Tracks' })).toHaveCount(0)
    await dialog.getByRole('radio', { name: 'Choose tracks' }).check()
    await expect(dialog.getByRole('button', { name: 'Create request' })).toBeDisabled()
    await expect(dialog.getByRole('status').filter({ hasText: 'of 3 selected' })).toHaveText('0 of 3 selected')
    await expect(dialog.getByRole('list', { name: 'Tracks' }).getByRole('listitem').first()).toContainText('4:11')
    await dialog.getByRole('checkbox', { name: /Second/ }).check()
    await dialog.getByRole('checkbox', { name: /Third/ }).check()
    await expect(dialog.getByRole('button', { name: 'Create request' })).toBeEnabled()
    await dialog.getByRole('button', { name: 'Create request' }).click()
    await expect(dialog.getByRole('status').filter({ hasText: 'Album requested' })).toBeVisible()
    expect(posts.body.selection).toEqual({ tracks: [{ id: 't2', position: 2, title: 'Second' }, { id: 't3', position: 3, title: 'Third' }] })
})

test('choosing the whole album sends no selection', async ({ page }) => {
    const posts: { body?: any } = {}
    const dialog = await open(page, album, 'https://musicbrainz.org/release-group/x', posts)
    await dialog.getByRole('button', { name: 'Create request' }).click()
    await expect(dialog.getByRole('status').filter({ hasText: 'Album requested' })).toBeVisible()
    expect(posts.body.selection).toBeUndefined()
})

test('an album without a tracklist offers only the whole album', async ({ page }) => {
    const posts: { body?: any } = {}
    const dialog = await open(page, { ...album, Music: { ...album.Music, tracks: undefined } }, 'https://musicbrainz.org/release-group/x', posts)
    await expect(dialog.getByRole('radiogroup', { name: 'What to request' })).toHaveCount(0)
    await expect(dialog.getByRole('button', { name: 'Create request' })).toBeEnabled()
})

test('a series is requested by season, and a whole season absorbs its episodes', async ({ page }) => {
    const posts: { body?: any } = {}
    const dialog = await open(page, series, 'https://www.themoviedb.org/tv/1', posts)
    await dialog.getByRole('radio', { name: 'Choose seasons or episodes' }).check()
    await expect(dialog.getByRole('button', { name: 'Request series' })).toBeDisabled()
    // One episode of season 1, then the rest of it: the season is selected whole.
    await dialog.locator('details').first().locator('summary').click()
    await dialog.getByRole('checkbox', { name: /Pilot/ }).check()
    await expect(dialog.getByRole('checkbox', { name: 'Season 1' })).not.toBeChecked()
    await expect(dialog.getByRole('status').filter({ hasText: 'episode' })).toHaveText('0 seasons, 1 episode')
    await dialog.getByRole('checkbox', { name: /Second/ }).check()
    await expect(dialog.getByRole('checkbox', { name: 'Season 1' })).toBeChecked()
    await expect(dialog.getByRole('status').filter({ hasText: 'episode' })).toHaveText('1 season, 0 episodes')
    // A single episode of season 2.
    await dialog.locator('details').nth(1).locator('summary').click()
    await dialog.getByRole('checkbox', { name: /Finale/ }).check()
    await dialog.getByRole('button', { name: 'Request series' }).click()
    await expect(dialog.getByRole('status').filter({ hasText: 'Series requested' })).toBeVisible()
    expect(posts.body.selection).toEqual({ seasons: [1], episodes: [{ season: 2, episode: 2 }] })
})

test('unchecking one episode of a whole season keeps the others', async ({ page }) => {
    const posts: { body?: any } = {}
    const dialog = await open(page, series, 'https://www.themoviedb.org/tv/1', posts)
    await dialog.getByRole('radio', { name: 'Choose seasons or episodes' }).check()
    await dialog.getByRole('checkbox', { name: 'Season 2' }).check()
    await dialog.locator('details').nth(1).locator('summary').click()
    await dialog.getByRole('checkbox', { name: /Return/ }).uncheck()
    await dialog.getByRole('button', { name: 'Request series' }).click()
    await expect(dialog.getByRole('status').filter({ hasText: 'Series requested' })).toBeVisible()
    expect(posts.body.selection).toEqual({ episodes: [{ season: 2, episode: 2 }] })
})

test('a second person can add seasons to an open partial request, but not the ones already requested', async ({ page }) => {
    const posts: { body?: any } = {}
    const shared = { availability: 'unknown', canRequest: false, canSubscribe: false, canExtend: true, request: { id: 'r1', status: 'requested', subscribed: false, selection: { seasons: [1] } } }
    const dialog = await open(page, { ...series, Preview: shared }, 'https://www.themoviedb.org/tv/1', posts, shared)
    await dialog.getByRole('radio', { name: 'Choose seasons or episodes' }).check()
    await expect(dialog.getByRole('checkbox', { name: 'Season 1' })).toBeDisabled()
    await expect(dialog.getByText('Already requested')).toBeVisible()
    await dialog.getByRole('checkbox', { name: 'Season 2' }).check()
    await page.route('**/api/v1/requests', route => { posts.body = route.request().postDataJSON(); return route.fulfill({ json: { Created: false, Merged: true, Request: { ID: 'r1', Status: 'requested' } } }) })
    await dialog.getByRole('button', { name: 'Add to request' }).click()
    await expect(dialog.getByRole('status').filter({ hasText: 'Added to the existing request' })).toBeVisible()
    expect(posts.body.selection).toEqual({ seasons: [2] })
})

test('the request list says what part of an item was requested', async ({ page }) => {
    await page.route('**/api/v1/**', route => {
        const path = new URL(route.request().url()).pathname
        if (path.endsWith('/auth/me')) return route.fulfill({ json: { user: { id: 'u1', role: 'requester', displayName: 'U' }, csrfToken: 'c' } })
        if (path.endsWith('/requests')) return route.fulfill({ json: { nextCursor: null, items: [
            { ID: 'a', CatalogItemID: 'ca', Title: 'Some album', MediaType: 'album', ArtworkURL: '', ArtworkKind: 'square', Status: 'requested', Version: 1, RequestedBy: 'u1', Note: '', availability: 'unknown', Platforms: { any: true }, CreatedAt: '2026-10-28T12:00:00Z', subscribed: true, subscriptionSource: 'requester', selection: { tracks: [{ id: 't1', title: 'First' }, { id: 't2', title: 'Second' }] } },
            { ID: 'b', CatalogItemID: 'cb', Title: 'Some series', MediaType: 'tv', ArtworkURL: '', ArtworkKind: 'poster', Status: 'requested', Version: 1, RequestedBy: 'u1', Note: '', availability: 'unknown', Platforms: { any: true }, CreatedAt: '2026-10-28T12:00:00Z', subscribed: true, subscriptionSource: 'requester', selection: { seasons: [1, 3], episodes: [{ season: 2, episode: 4 }] } },
            { ID: 'c', CatalogItemID: 'cc', Title: 'Whole movie', MediaType: 'movie', ArtworkURL: '', ArtworkKind: 'poster', Status: 'requested', Version: 1, RequestedBy: 'u1', Note: '', availability: 'unknown', Platforms: { any: true }, CreatedAt: '2026-10-28T12:00:00Z', subscribed: true, subscriptionSource: 'requester', selection: {} },
        ] } })
        return route.fulfill({ json: {} })
    })
    await page.goto('/requests')
    await expect(page.getByText('2 tracks: First, Second')).toBeVisible()
    await expect(page.getByText('Season 1, Season 3 · S2E4')).toBeVisible()
    await expect(page.getByRole('listitem').filter({ hasText: 'Whole movie' })).not.toContainText('Requested:')
})

test('the pickers have no serious accessibility violations', async ({ page }) => {
    const posts: { body?: any } = {}
    const dialog = await open(page, series, 'https://www.themoviedb.org/tv/1', posts)
    await dialog.getByRole('radio', { name: 'Choose seasons or episodes' }).check()
    await dialog.locator('details').first().locator('summary').click()
    const results = await new AxeBuilder({ page }).include('[role="dialog"]').analyze()
    expect(results.violations.filter(v => v.impact === 'serious' || v.impact === 'critical')).toEqual([])
})
