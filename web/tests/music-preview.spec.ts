import { expect, test, type Page } from '@playwright/test'
import AxeBuilder from '@axe-core/playwright'
import { capabilitiesFor } from './capabilities'

const album = {
    Item: { ID: '44444444-4444-4444-8444-444444444444', MediaType: 'album', Provider: 'musicbrainz', Title: 'Radiohead — Kid A', Year: 2000, ArtworkURL: '', ArtworkKind: 'square', ExternalURL: 'https://musicbrainz.org/release-group/11111111-1111-4111-8111-111111111111' }, Platforms: null,
    Music: { artists: ['Radiohead'], primaryType: 'Album', secondaryTypes: ['Live'], releaseDate: '2000-10-02', genres: ['electronic', 'art rock'], score: 4.5, ratingCount: 22 },
    Preview: { availability: 'unknown', canRequest: true, canSubscribe: false },
}
const song = {
    Item: { ...album.Item, ID: '55555555-5555-4555-8555-555555555555', MediaType: 'song', Title: 'Radiohead — Everything in Its Right Place', ExternalURL: 'https://musicbrainz.org/recording/22222222-2222-4222-8222-222222222222' },
    Music: { artists: ['Radiohead'], releaseDate: '2000-10-02', lengthSeconds: 251, appearsOn: ['Kid A', 'Amnesiac'] },
    Preview: album.Preview,
}

async function open(page: Page, resolved: any, role = 'requester', onRequest?: (body: unknown) => void) {
    await page.route('**/api/v1/**', route => {
        const path = new URL(route.request().url()).pathname
        if (path.endsWith('/auth/me')) return route.fulfill({ json: { user: { id: 'user-1', role, displayName: 'User' }, capabilities: capabilitiesFor(role), csrfToken: 'csrf-token' } })
        if (path.endsWith('/resolve')) return route.fulfill({ json: resolved })
        if (path.endsWith('/requests') && route.request().method() === 'POST') {
            onRequest?.(route.request().postDataJSON())
            return route.fulfill({ status: 201, json: { Created: true, Request: { ID: '33333333-3333-4333-8333-333333333333', Status: 'requested' } } })
        }
        if (path.includes('/preview')) return route.fulfill({ json: { availability: 'unknown', canRequest: false, canSubscribe: false, request: { id: '33333333-3333-4333-8333-333333333333', status: 'requested', subscribed: true } } })
        return route.fulfill({ json: { items: [], nextCursor: null } })
    })
    await page.goto('/')
    await page.getByPlaceholder('Search or paste a link…').fill(resolved.Item.ExternalURL)
    await page.getByRole('button', { name: 'Preview', exact: true }).click()
    return page.getByRole('dialog', { name: new RegExp(resolved.Item.Title.split(' — ')[1]) })
}

test('album preview shows the artist, release type, genres, date and rating', async ({ page }) => {
    const dialog = await open(page, album)
    await expect(dialog).toContainText('Album preview')
    await expect(dialog).toContainText('Artist: Radiohead')
    for (const text of ['Album', 'Live', 'electronic', 'art rock', 'Release date:', '4.5/5', '22 ratings']) await expect(dialog).toContainText(text)
    await expect(dialog.getByRole('link', { name: 'MusicBrainz (opens in a new tab)' })).toHaveAttribute('href', album.Item.ExternalURL)
    await expect(dialog.getByLabel('Any platform')).toHaveCount(0)
    // Release type and genres are separate tags.
    await expect(dialog.getByRole('list', { name: 'Type and genres' }).getByRole('listitem')).toHaveText(['Album', 'Live', 'electronic', 'art rock'])
    await expect(dialog.getByRole('list', { name: 'Library availability' })).toHaveText('Availability unknown')
})

test('song preview shows its length and the releases it appears on', async ({ page }) => {
    const dialog = await open(page, song)
    await expect(dialog).toContainText('Song preview')
    await expect(dialog).toContainText('Length: 4:11')
    await expect(dialog).toContainText('Appears on: Kid A, Amnesiac')
    await expect(dialog).toContainText('MusicBrainz · Not yet rated')
})

test('requesting an album stays in the dialog, sends the note, and shows the shared request', async ({ page }) => {
    let sent: unknown
    const dialog = await open(page, album, 'requester', body => { sent = body })
    await dialog.getByRole('button', { name: 'Add a note' }).click()
    await dialog.getByLabel('Note (optional)').fill('For the car')
    await dialog.getByRole('button', { name: 'Create request' }).click()
    await expect(dialog.getByText('Album requested')).toBeVisible()
    expect(sent).toEqual({ catalogId: album.Item.ID, note: 'For the car', platforms: { any: true } })
    await expect(dialog.getByRole('link', { name: 'View request' })).toBeVisible()
})

test('music preview has no accessibility violations', async ({ page }) => {
    const dialog = await open(page, album)
    await expect(dialog).toBeVisible()
    expect((await new AxeBuilder({ page }).include('[role="dialog"]').analyze()).violations).toEqual([])
})

test('album whose cover art fails to load shows a placeholder and no blurred banner', async ({ page }) => {
    await page.route('https://coverartarchive.org/**', route => route.abort())
    const album = { Item: { ID: '11111111-1111-4111-8111-111111111111', MediaType: 'album', Provider: 'musicbrainz', Title: 'No Cover', Year: 2026, ArtworkURL: 'https://coverartarchive.org/release-group/x/front-500', ArtworkKind: 'square', ExternalURL: 'https://musicbrainz.org/release/x' }, Platforms: [], Music: { primaryType: 'Album', artists: ['Someone'] }, Preview: { availability: 'unknown', canRequest: true, canSubscribe: false } }
    await page.route('**/api/v1/**', route => {
        const path = new URL(route.request().url()).pathname
        if (path.endsWith('/auth/me')) return route.fulfill({ json: { user: { id: 'u', role: 'requester', displayName: 'U' }, capabilities: capabilitiesFor('requester'), csrfToken: 'c' } })
        if (path.endsWith('/resolve')) return route.fulfill({ json: album })
        return route.fulfill({ json: { items: [], nextCursor: null } })
    })
    await page.goto('/')
    await page.getByPlaceholder('Search or paste a link…').fill('https://musicbrainz.org/release/x')
    await page.getByRole('button', { name: 'Preview', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: /No Cover/ })
    await expect(dialog.getByRole('img', { name: 'Cover unavailable' })).toBeVisible()
    await expect(dialog.locator('.movieBanner')).toHaveCount(0)
})
