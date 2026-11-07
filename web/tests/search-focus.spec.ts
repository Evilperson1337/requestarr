import { expect, test, type Page } from '@playwright/test'
import { capabilitiesFor } from './capabilities'

const box = 'Search or paste a link…'
const ID = '77777777-7777-4777-8777-777777777777'
const mbid = 'b1a9c0e9-d987-4042-ae91-78d6a3267d69'

// One search row and the preview it resolves to, per search type.
const cases = [
    { type: 'game', label: 'Games', title: 'The Witcher 3',
        row: { mediaType: 'game', provider: 'igdb', providerId: '1942', ref: 'https://www.igdb.com/games/the-witcher-3', title: 'The Witcher 3', year: 2015, subtitle: 'PC', artworkUrl: '', artworkKind: 'poster' },
        resolved: { Item: { ID, MediaType: 'game', Provider: 'igdb', ProviderID: '1942', Title: 'The Witcher 3', Year: 2015, ArtworkURL: '', ArtworkKind: 'poster', ExternalURL: 'https://www.igdb.com/games/the-witcher-3' }, Platforms: [{ providerId: '6', key: 'igdb:6', name: 'PC' }], Game: { overview: '', genres: [], releaseDate: '2015-05-19', developers: [] }, Preview: { availability: 'unknown', canRequest: true, canSubscribe: false } } },
    { type: 'album', label: 'Albums', title: 'The Dark Side of the Moon',
        row: { mediaType: 'album', provider: 'musicbrainz', providerId: mbid, ref: `https://musicbrainz.org/release-group/${mbid}`, title: 'The Dark Side of the Moon', year: 1973, subtitle: 'Pink Floyd', artworkUrl: '', artworkKind: 'square' },
        resolved: { Item: { ID, MediaType: 'album', Provider: 'musicbrainz', ProviderID: mbid, Title: 'Pink Floyd — The Dark Side of the Moon', Year: 1973, ArtworkURL: '', ArtworkKind: 'square', ExternalURL: `https://musicbrainz.org/release-group/${mbid}` }, Platforms: [], Music: { Artists: ['Pink Floyd'], PrimaryType: 'Album', Genres: [], Tracks: [] } } },
    { type: 'song', label: 'Songs', title: 'Money',
        row: { mediaType: 'song', provider: 'musicbrainz', providerId: mbid, ref: `https://musicbrainz.org/recording/${mbid}`, title: 'Money', year: 1973, subtitle: 'Pink Floyd · 6:22', artworkUrl: '', artworkKind: 'square' },
        resolved: { Item: { ID, MediaType: 'song', Provider: 'musicbrainz', ProviderID: mbid, Title: 'Pink Floyd — Money', Year: 1973, ArtworkURL: '', ArtworkKind: 'square', ExternalURL: `https://musicbrainz.org/recording/${mbid}` }, Platforms: [], Music: { Artists: ['Pink Floyd'], Genres: [], Tracks: [] } } },
    { type: 'audiobook', label: 'Audiobooks', title: 'Project Hail Mary',
        row: { mediaType: 'audiobook', provider: 'audible', providerId: 'B08G9PRS1K', ref: 'https://www.audible.com/pd/B08G9PRS1K', title: 'Project Hail Mary', year: 2021, subtitle: 'Andy Weir', artworkUrl: '', artworkKind: 'square' },
        resolved: { Item: { ID, MediaType: 'audiobook', Provider: 'audible', ProviderID: 'B08G9PRS1K', Title: 'Project Hail Mary', Year: 2021, ArtworkURL: '', ArtworkKind: 'square', ExternalURL: 'https://www.audible.com/pd/B08G9PRS1K', Metadata: { asin: 'B08G9PRS1K', region: 'us', authors: ['Andy Weir'], narrators: [], abridged: false, runtimeMinutes: 970 } }, Platforms: null, Preview: { availability: 'unknown', canRequest: true, canSubscribe: false } } },
]

async function open(page: Page, c: (typeof cases)[number], searches: URL[] = []) {
    await page.route('**/api/v1/**', async route => {
        const url = new URL(route.request().url())
        const path = url.pathname.replace('/api/v1', '')
        if (path === '/auth/me') return route.fulfill({ json: { user: { id: 'user-1', role: 'requester', displayName: 'User' }, capabilities: capabilitiesFor('requester'), csrfToken: 'csrf-token' } })
        if (path === '/search/types') return route.fulfill({ json: { types: ['movie_tv', 'game', 'album', 'song', 'audiobook'] } })
        if (path === '/search') { searches.push(url); return route.fulfill({ json: { results: [c.row], hasMore: false } }) }
        if (path === '/resolve') return route.fulfill({ json: c.resolved })
        return route.fulfill({ json: { items: [], nextCursor: null } })
    })
    await page.goto('/')
    await page.getByRole('radio', { name: c.label }).check()
}

for (const c of cases) {
    for (const close of ['Escape', 'Close preview']) {
        test(`closing a ${c.type} preview with ${close} returns focus to the result row`, async ({ page }) => {
            await open(page, c)
            await page.getByPlaceholder(box).fill(c.title)
            await page.getByPlaceholder(box).press('Enter')
            const target = page.getByRole('list', { name: 'Search results' }).getByRole('button', { name: new RegExp(`^${c.title}`) })
            await target.click()
            const dialog = page.getByRole('dialog')
            await expect(dialog).toBeVisible()
            if (close === 'Escape') await page.keyboard.press('Escape')
            else await dialog.getByRole('button', { name: close, exact: true }).click()
            await expect(dialog).toHaveCount(0)
            await expect(target).toBeFocused()
        })
    }
}

test('the hero text names the search types offered and the paste path', async ({ page }) => {
    await open(page, cases[0])
    const hero = page.locator('.hero > p').first()
    for (const word of ['movie or show', 'album', 'song', 'game', 'audiobook', 'paste']) await expect(hero).toContainText(word)
})

test('the hero text only offers search for the types the server has', async ({ page }) => {
    await page.route('**/api/v1/**', async route => {
        const path = new URL(route.request().url()).pathname.replace('/api/v1', '')
        if (path === '/auth/me') return route.fulfill({ json: { user: { id: 'user-1', role: 'requester', displayName: 'User' }, capabilities: capabilitiesFor('requester'), csrfToken: 'csrf-token' } })
        if (path === '/search/types') return route.fulfill({ json: { types: ['game'] } })
        return route.fulfill({ json: { items: [], nextCursor: null } })
    })
    await page.goto('/')
    const hero = page.locator('.hero > p').first()
    await expect(hero).toContainText('game')
    await expect(hero).not.toContainText('movie')
    await expect(hero).not.toContainText('album')
})

test('Include singles & EPs lives in the URL: set, reloaded, and restored by Back and Forward', async ({ page }) => {
    const searches: URL[] = []
    await open(page, cases[1], searches)
    const input = page.getByPlaceholder(box)
    const singles = page.getByRole('checkbox', { name: 'Include singles & EPs' })
    await input.fill('moon')
    await input.press('Enter')
    await expect(page).toHaveURL(/\?q=moon&type=album$/)
    await singles.check()
    await expect(page).toHaveURL(/\?q=moon&type=album&singles=true$/)
    expect(searches.at(-1)?.searchParams.get('singles')).toBe('true')

    await page.reload()
    await expect(singles).toBeChecked()
    await expect(input).toHaveValue('moon')
    await expect.poll(() => searches.at(-1)?.searchParams.get('singles')).toBe('true')

    await singles.uncheck()
    await expect(page).toHaveURL(/\?q=moon&type=album$/)
    await page.goBack()
    await expect(page).toHaveURL(/\/$/)
    await page.goForward()
    await expect(page).toHaveURL(/\?q=moon&type=album$/)
    await expect(singles).not.toBeChecked()
    await singles.check()
    await page.goBack()
    await page.goForward()
    await expect(page).toHaveURL(/singles=true/)
    await expect(singles).toBeChecked()
})
