import { expect, test, type Page } from '@playwright/test'

type Handler = (path: string, method: string, body: any) => unknown

async function mockApp(page: Page, handler: Handler, role = 'requester') {
    await page.route('**/api/v1/**', route => {
        const request = route.request()
        const path = new URL(request.url()).pathname.replace('/api/v1', '')
        if (path === '/setup/status') return route.fulfill({ json: { complete: true, required: false } })
        if (path === '/auth/me') return route.fulfill({ json: { user: { id: 'user-1', role, email: 'user@example.test', displayName: 'User' }, csrfToken: 'csrf-token' } })
        const result = handler(path, request.method(), request.postDataJSON())
        return route.fulfill(result === undefined ? { status: 404, json: { error: { code: 'not_found', message: 'unmocked' } } } : { json: result })
    })
}

// Mirrors the API's mixed casing: catalog fields are PascalCase, platform fields camelCase.
const resolved = (mediaType: string) => ({
    Item: { ID: 'catalog-1', MediaType: mediaType, Provider: 'fixture', Title: 'Outer Wilds', Year: 2019, ArtworkURL: '', ArtworkKind: 'poster', ExternalURL: '' },
    Platforms: [{ providerId: '6', key: 'pc', name: 'PC' }, { providerId: '48', key: 'ps4', name: 'PlayStation 4' }],
})

test('home resolves a link, previews it in a labelled dialog, and creates a request', async ({ page }) => {
    const calls: { path: string; body: any }[] = []
    await mockApp(page, (path, method, body) => {
        if (path === '/resolve') return resolved('movie')
        if (path === '/requests' && method === 'POST') { calls.push({ path, body }); return { Created: true, Request: { ID: '22222222-2222-4222-8222-222222222222', Status: 'requested' } } }
        if (path.startsWith('/catalog/')) return { availability: 'unknown', canRequest: false, canSubscribe: false, request: { id: '22222222-2222-4222-8222-222222222222', status: 'requested', subscribed: true } }
        if (path === '/requests') return { items: [], nextCursor: null }
    })
    await page.goto('/')
    await page.getByPlaceholder('Search or paste a link…').fill('https://www.themoviedb.org/movie/11')
    await page.getByRole('button', { name: 'Preview' }).click()
    const dialog = page.getByRole('dialog', { name: /Outer Wilds/ })
    await expect(dialog).toBeVisible()
    await dialog.getByRole('button', { name: 'Add a note' }).click()
    await dialog.getByLabel('Note (optional)').fill('Please add')
    await dialog.getByRole('button', { name: 'Request movie', exact: true }).click()
    await expect(dialog).toContainText('Movie requested')
    await expect(page).toHaveURL(/^http:\/\/127\.0\.0\.1:\d+\/$/)
    expect(calls).toEqual([{ path: '/requests', body: { catalogId: 'catalog-1', note: 'Please add', platforms: { any: true } } }])
})

for (const mediaType of ['album', 'song']) test(`${mediaType} previews like the other media types and keeps the note`, async ({ page }) => {
    const calls: unknown[] = []
    await mockApp(page, (path, method, body) => {
        if (path === '/resolve') return resolved(mediaType)
        if (path === '/requests' && method === 'POST') { calls.push(body); return { Created: true, Request: { ID: 'request-1', Status: 'requested' } } }
        if (path.includes('/preview')) return { availability: 'unknown', canRequest: false, canSubscribe: false, request: { id: 'request-1', status: 'requested', subscribed: true } }
        if (path === '/requests') return { items: [], nextCursor: null }
    })
    await page.goto('/')
    await page.getByPlaceholder('Search or paste a link…').fill(`https://musicbrainz.org/${mediaType === 'album' ? 'release-group' : 'recording'}/11111111-1111-4111-8111-111111111111`)
    await page.getByRole('button', { name: 'Preview', exact: true }).click()
    const dialog = page.getByRole('dialog')
    await dialog.getByRole('button', { name: 'Add a note' }).click()
    await dialog.getByLabel('Note (optional)').fill('Please add')
    await dialog.getByRole('button', { name: 'Create request', exact: true }).click()
    await expect(dialog).toContainText(`${mediaType === 'album' ? 'Album' : 'Song'} requested`)
    expect(calls).toEqual([{ catalogId: 'catalog-1', note: 'Please add', platforms: { any: true } }])
})

test('game request modal enforces Any-or-explicit platforms and can be cancelled', async ({ page }) => {
    await mockApp(page, path => path === '/resolve' ? resolved('game') : undefined)
    await page.goto('/')
    await page.getByPlaceholder('Search or paste a link…').fill('https://www.igdb.com/games/outer-wilds')
    await page.getByRole('button', { name: 'Preview' }).click()
    const dialog = page.getByRole('dialog')
    await dialog.getByLabel('PC').check()
    await expect(dialog.getByLabel('Any platform')).not.toBeChecked()
    await dialog.getByLabel('PC').uncheck()
    await expect(dialog.getByRole('button', { name: 'Create request' })).toBeDisabled()
    await dialog.getByLabel('Any platform').check()
    await expect(dialog.getByRole('button', { name: 'Create request' })).toBeEnabled()
    await dialog.getByRole('button', { name: 'Close preview' }).click()
    await expect(dialog).toHaveCount(0)
})

test('resolve failure is announced without opening the dialog', async ({ page }) => {
    await mockApp(page, () => undefined)
    await page.route('**/api/v1/resolve', route => route.fulfill({ status: 422, json: { error: { code: 'unsupported', message: 'That link is not supported.' } } }))
    await page.goto('/')
    await page.getByPlaceholder('Search or paste a link…').fill('https://example.test/x')
    await page.getByRole('button', { name: 'Preview' }).click()
    await expect(page.getByRole('alert')).toContainText('not supported')
    await expect(page.getByRole('dialog')).toHaveCount(0)
})

test('notifications list unread items and mark one read with CSRF', async ({ page }) => {
    const reads: (string | undefined)[] = []
    await mockApp(page, path => path === '/notifications' ? {
        items: [
            { id: 'n1', title: 'Arrival is available', body: 'Now in Jellyfin', createdAt: '2026-10-01T12:00:00Z' },
            { id: 'n2', title: 'Old news', body: 'Seen', createdAt: '2026-09-01T12:00:00Z', readAt: '2026-09-02T00:00:00Z' },
        ]
    } : undefined)
    await page.route('**/api/v1/notifications/*/read', route => {
        reads.push(route.request().headers()['x-csrf-token'])
        return route.fulfill({ status: 204 })
    })
    await page.goto('/notifications')
    await expect(page.getByRole('heading', { name: 'Notifications', exact: true })).toBeVisible()
    await expect(page.getByText('Arrival is available')).toBeVisible()
    await expect(page.getByRole('button', { name: 'Mark read' })).toHaveCount(1)
    await page.getByRole('button', { name: 'Mark read' }).click()
    await expect(page.getByRole('button', { name: 'Mark read' })).toHaveCount(0)
    expect(reads).toEqual(['csrf-token'])
})

test('empty notifications are explained', async ({ page }) => {
    await mockApp(page, path => path === '/notifications' ? { items: [] } : undefined)
    await page.goto('/notifications')
    await expect(page.getByText('No notifications yet.')).toBeVisible()
})

test('notification load failure is announced', async ({ page }) => {
    await mockApp(page, () => undefined)
    await page.goto('/notifications')
    await expect(page.getByRole('alert')).toContainText('Could not load notifications')
})

test('non-admins never see Get started or Settings navigation, and Settings is not rendered for them', async ({ page }) => {
    await mockApp(page, path => path === '/requests' ? { items: [], nextCursor: null } : undefined)
    await page.goto('/settings')
    await expect(page.getByRole('link', { name: 'Settings' })).toHaveCount(0)
    await expect(page.getByRole('link', { name: 'Get started', exact: true })).toHaveCount(0)
    await expect(page.getByRole('heading', { name: 'Operational status' })).toHaveCount(0)
    await expect(page.getByRole('heading', { name: 'Notifications', exact: true })).toBeVisible() // /settings falls back to a page every user may see
})
