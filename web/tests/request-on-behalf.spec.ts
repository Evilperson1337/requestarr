import { expect, test, type Page } from '@playwright/test'
import AxeBuilder from '@axe-core/playwright'
import { capabilitiesFor } from './capabilities'

const movie = {
    Item: { ID: '11111111-1111-4111-8111-111111111111', MediaType: 'movie', Provider: 'tmdb', Title: 'Fixture movie', Year: 2026, ArtworkURL: '', ArtworkKind: 'poster', ExternalURL: 'https://www.themoviedb.org/movie/1762506' }, Platforms: [],
    Movie: { overview: 'A synopsis.', runtime: 145, genres: ['Adventure'], releaseDate: '2026-01-01', releaseStatus: 'Released', score: 7.8, ratingCount: 120 },
    Preview: { availability: 'unknown', canRequest: true, canSubscribe: false },
}
const alice = '33333333-3333-4333-8333-333333333333'
const people = [
    { id: 'user-1', email: 'me@example.test', displayName: 'Me', role: 'admin', canRequest: true, builtin: false, createdAt: '2026-01-01T00:00:00Z' },
    { id: alice, email: 'alice@example.test', displayName: 'Alice', role: 'requester', canRequest: true, builtin: false, createdAt: '2026-01-01T00:00:00Z' },
    { id: '44444444-4444-4444-8444-444444444444', email: 'vic@example.test', displayName: 'Vic Viewer', role: 'viewer', canRequest: false, builtin: false, createdAt: '2026-01-01T00:00:00Z' },
]

async function openMovie(page: Page, role: string) {
    await page.route('**/api/v1/**', route => {
        const path = new URL(route.request().url()).pathname
        if (path.endsWith('/auth/me')) return route.fulfill({ json: { user: { id: 'user-1', role, displayName: 'User' }, capabilities: capabilitiesFor(role), csrfToken: 'csrf-token' } })
        if (path.endsWith('/resolve')) return route.fulfill({ json: movie })
        if (path.endsWith('/admin/users')) return route.fulfill({ json: { users: people } })
        return route.fulfill({ json: { items: [], nextCursor: null } })
    })
    await page.goto('/')
    await page.getByPlaceholder('Search or paste a link…').fill('https://www.themoviedb.org/movie/1762506-band-of-brothers-legacy')
    await page.getByRole('button', { name: 'Preview', exact: true }).click()
    return page.getByRole('dialog', { name: /Fixture movie/ })
}

test('the person picker is shown to an administrator and offers only people who may request', async ({ page }) => {
    const dialog = await openMovie(page, 'admin')
    const picker = dialog.getByLabel('Request for')
    await expect(picker).toBeVisible()
    await expect(picker.locator('option')).toHaveText(['Myself', 'Alice'])
})

test('requesters never see the person picker and never fetch the people list', async ({ page }) => {
    let listed = false
    await page.route('**/api/v1/admin/users', route => { listed = true; return route.fulfill({ status: 403, json: { error: { code: 'forbidden', message: 'no' } } }) })
    const dialog = await openMovie(page, 'requester')
    await expect(dialog.getByRole('button', { name: 'Request movie', exact: true })).toBeVisible()
    await expect(dialog.getByLabel('Request for')).toHaveCount(0)
    expect(listed).toBe(false)
})

test('viewers cannot request and see no person picker', async ({ page }) => {
    const dialog = await openMovie(page, 'viewer')
    await expect(dialog).toContainText('cannot create Requests')
    await expect(dialog.getByLabel('Request for')).toHaveCount(0)
})

test('the chosen person is sent as the requester', async ({ page }) => {
    const dialog = await openMovie(page, 'admin')
    let body: Record<string, unknown> = {}
    await page.route('**/api/v1/requests', route => {
        body = route.request().postDataJSON()
        return route.fulfill({ status: 201, json: { Created: true, Request: { ID: '22222222-2222-4222-8222-222222222222', Status: 'requested' } } })
    })
    await dialog.getByLabel('Request for').selectOption({ label: 'Alice' })
    await dialog.getByRole('button', { name: 'Request movie', exact: true }).click()
    await expect(dialog).toContainText('Movie requested')
    expect(body.onBehalfOf).toBe(alice)
})

test('asking for myself sends no person', async ({ page }) => {
    const dialog = await openMovie(page, 'admin')
    let body: Record<string, unknown> = {}
    await page.route('**/api/v1/requests', route => {
        body = route.request().postDataJSON()
        return route.fulfill({ status: 201, json: { Created: true, Request: { ID: '22222222-2222-4222-8222-222222222222', Status: 'requested' } } })
    })
    await dialog.getByRole('button', { name: 'Request movie', exact: true }).click()
    await expect(dialog).toContainText('Movie requested')
    expect('onBehalfOf' in body).toBe(false)
})

for (const [name, size] of [['desktop', { width: 1280, height: 900 }], ['phone', { width: 390, height: 780 }]] as const) {
    test(`the person picker has no accessibility violations at ${name} width`, async ({ page }) => {
        await page.setViewportSize(size)
        const dialog = await openMovie(page, 'admin')
        await expect(dialog.getByLabel('Request for')).toBeVisible()
        const results = await new AxeBuilder({ page }).include('[role="dialog"]').analyze()
        expect(results.violations).toEqual([])
    })
}

test('the request list says when a request was made on the requester behalf', async ({ page }) => {
    await page.route('**/api/v1/**', route => {
        const path = new URL(route.request().url()).pathname
        if (path.endsWith('/auth/me')) return route.fulfill({ json: { user: { id: alice, role: 'requester', displayName: 'Alice' }, capabilities: capabilitiesFor('requester'), csrfToken: 'csrf-token' } })
        if (path.endsWith('/requests')) return route.fulfill({ json: { items: [{ ID: 'r1', CatalogItemID: 'c1', Title: 'Fixture movie', MediaType: 'movie', ArtworkURL: '', ArtworkKind: 'poster', Status: 'requested', Version: 1, RequestedBy: alice, madeOnBehalfBy: 'user-9', madeOnBehalfByName: 'Boss', Note: '', availability: 'unknown', Platforms: { any: true }, CreatedAt: '2026-01-01T00:00:00Z', subscribed: true, subscriptionSource: 'requester' }], nextCursor: null } })
        return route.fulfill({ json: {} })
    })
    await page.goto('/requests')
    await expect(page.getByText('Made on behalf of the requester by Boss')).toBeVisible()
})
