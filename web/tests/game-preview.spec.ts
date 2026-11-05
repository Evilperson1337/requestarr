import { expect, test, type Page } from '@playwright/test'
import AxeBuilder from '@axe-core/playwright'

const game = {
    Item: { ID: '22222222-2222-4222-8222-222222222222', MediaType: 'game', Provider: 'igdb', Title: 'Fixture game', Year: 2026, ArtworkURL: '', ArtworkKind: 'poster', ExternalURL: 'https://www.igdb.com/games/fixture-game' },
    Platforms: [{ providerId: '6', key: 'igdb:6', name: 'PC' }, { providerId: '167', key: 'igdb:167', name: 'PlayStation 5' }],
    Game: { overview: 'A game synopsis. '.repeat(30), genres: ['Shooter'], releaseDate: '2026-10-23', developers: ['Fixture Studio'], score: 81.4, ratingCount: 40 },
    Preview: { availability: 'unknown', canRequest: true, canSubscribe: false },
}

async function openGame(page: Page, resolved: unknown = game, role = 'requester', onRequest?: (body: unknown) => void) {
    await page.route('**/api/v1/**', async route => {
        const path = new URL(route.request().url()).pathname
        if (path.endsWith('/setup/status')) return route.fulfill({ json: { complete: true, required: false, wizardComplete: true } })
        if (path.endsWith('/auth/me')) return route.fulfill({ json: { user: { id: 'user-1', role, displayName: 'User' }, csrfToken: 'csrf-token' } })
        if (path.endsWith('/resolve')) return route.fulfill({ json: resolved })
        if (path.endsWith('/requests') && route.request().method() === 'POST') {
            onRequest?.(route.request().postDataJSON())
            return route.fulfill({ status: 201, json: { Created: true, Request: { ID: '33333333-3333-4333-8333-333333333333', Status: 'requested' } } })
        }
        if (path.includes('/preview')) return route.fulfill({ json: { availability: 'unknown', canRequest: false, canSubscribe: false, request: { id: '33333333-3333-4333-8333-333333333333', status: 'requested', subscribed: true } } })
        return route.fulfill({ json: { items: [], nextCursor: null } })
    })
    await page.goto('/')
    await page.getByPlaceholder('Search or paste a link…').fill('https://www.igdb.com/games/fixture-game')
    await page.getByRole('button', { name: 'Preview', exact: true }).click()
    return page.getByRole('dialog', { name: /Fixture game/ })
}

test('game preview shows the same rich facts as a movie, with IGDB sourcing', async ({ page }) => {
    const dialog = await openGame(page)
    await expect(dialog).toContainText('Game preview')
    await expect(dialog).toContainText('Shooter')
    await expect(dialog).toContainText('81/100')
    await expect(dialog).toContainText('40 ratings')
    await expect(dialog).toContainText('Developer: Fixture Studio')
    await expect(dialog.getByRole('button', { name: /Show (more|less)/ })).toHaveCount(0)
    await expect(dialog.getByRole('list', { name: 'Genres' }).getByRole('listitem')).toHaveText(['Shooter'])
    await expect(dialog.getByRole('list', { name: 'Library availability' })).toHaveText('Availability unknown')
    await expect(dialog.getByRole('link', { name: 'IGDB (opens in a new tab)' })).toHaveAttribute('href', game.Item.ExternalURL)
})

test('game request needs a platform choice and sends it with the note', async ({ page }) => {
    let sent: any
    const dialog = await openGame(page, game, 'requester', body => { sent = body })
    await dialog.getByLabel('PlayStation 5').check()
    await dialog.getByLabel('PlayStation 5').uncheck()
    await expect(dialog.getByRole('button', { name: 'Create request' })).toBeDisabled()
    await dialog.getByLabel('PC', { exact: true }).check()
    await dialog.getByRole('button', { name: 'Add a note' }).click()
    await dialog.getByLabel('Note (optional)').fill('For the weekend')
    await dialog.getByRole('button', { name: 'Create request' }).click()
    await expect(dialog.getByText('Game requested')).toBeVisible()
    expect(sent).toEqual({ catalogId: game.Item.ID, note: 'For the weekend', platforms: { any: false, platformKeys: ['igdb:6'] } })
    await expect(dialog.getByRole('link', { name: 'View request' })).toBeVisible()
})

test('a viewer sees the game state but cannot request it', async ({ page }) => {
    const dialog = await openGame(page, { ...game, Preview: { availability: 'unknown', canRequest: false, canSubscribe: false } }, 'viewer')
    await expect(dialog).toContainText('Your viewer role cannot create Requests. Ask an administrator to make you a requester.')
    await expect(dialog.getByRole('button', { name: 'Create request' })).toHaveCount(0)
})

test('game preview has no accessibility violations', async ({ page }) => {
    const dialog = await openGame(page)
    await expect(dialog).toBeVisible()
    const results = await new AxeBuilder({ page }).include('[role="dialog"]').analyze()
    expect(results.violations).toEqual([])
})

test('a game available on two media servers shows one tag per server', async ({ page }) => {
    const dialog = await openGame(page, { ...game, Preview: { availability: 'available', canRequest: false, canSubscribe: false, libraries: [{ name: 'RomM', url: 'https://romm.example.test/g/1' }, { name: 'Plex', url: 'https://plex.example.test/g/1' }] } })
    await expect(dialog.getByRole('list', { name: 'Library availability' }).getByRole('listitem')).toHaveText(['RomM: Available', 'Plex: Available'])
    await expect(dialog.getByRole('link', { name: 'RomM: Available' })).toHaveAttribute('href', 'https://romm.example.test/g/1')
})
