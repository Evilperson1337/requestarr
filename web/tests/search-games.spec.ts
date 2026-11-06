import { expect, test } from '@playwright/test'

const witcher = { mediaType: 'game', provider: 'igdb', providerId: '1942', ref: 'https://www.igdb.com/games/the-witcher-3-wild-hunt', title: 'The Witcher 3: Wild Hunt', year: 2015, subtitle: 'PC, PS4, XONE +1', artworkUrl: '', artworkKind: 'poster' }
const resolved = {
    Item: { ID: '22222222-2222-4222-8222-222222222222', MediaType: 'game', Provider: 'igdb', ProviderID: '1942', Title: 'The Witcher 3: Wild Hunt', Year: 2015, ArtworkURL: '', ArtworkKind: 'poster', ExternalURL: witcher.ref },
    Platforms: [{ providerId: '6', key: 'igdb:6', name: 'PC' }, { providerId: '167', key: 'igdb:167', name: 'PlayStation 5' }],
    Game: { overview: 'A witcher.', genres: [], releaseDate: '2015-05-19', developers: [] },
    Preview: { availability: 'unknown', canRequest: true, canSubscribe: false },
}

test('Games searches as you type, shows year and a platform hint, and a pick opens the game preview with platforms', async ({ page }) => {
    const types: string[] = []
    const resolves: string[] = []
    await page.route('**/api/v1/**', async route => {
        const url = new URL(route.request().url())
        const path = url.pathname.replace('/api/v1', '')
        if (path === '/auth/me') return route.fulfill({ json: { user: { id: 'user-1', role: 'requester', displayName: 'User' }, csrfToken: 'csrf-token' } })
        if (path === '/search/types') return route.fulfill({ json: { types: ['game'] } })
        if (path === '/search') { types.push(url.searchParams.get('type') ?? ''); return route.fulfill({ json: { results: [witcher], hasMore: false } }) }
        if (path === '/resolve') { resolves.push(route.request().postDataJSON().url); return route.fulfill({ json: resolved }) }
        return route.fulfill({ json: { items: [], nextCursor: null } })
    })
    await page.goto('/')
    await expect(page.getByRole('radiogroup', { name: 'Search for' }).getByRole('radio', { name: 'Games' })).toBeChecked()
    await page.getByPlaceholder('Search or paste a link…').fill('witcher')
    const row = page.getByRole('list', { name: 'Search results' }).getByRole('button', { name: /The Witcher 3.*2015.*PC, PS4, XONE \+1/ })
    await expect(row).toBeVisible()
    expect([...new Set(types)]).toEqual(['game'])
    await row.click()
    const dialog = page.getByRole('dialog', { name: /The Witcher 3/ })
    await expect(dialog).toBeVisible()
    await expect(dialog).toContainText('Game preview')
    await expect(dialog.getByText('PlayStation 5')).toBeVisible()
    expect(resolves).toEqual([witcher.ref])
})
