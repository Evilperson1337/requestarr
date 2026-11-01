import { expect, test, type Page } from '@playwright/test'

const row = (id: string, title: string, mediaType: string, status: string, over: object = {}) => ({
    ID: id, CatalogItemID: 'c' + id, Title: title, MediaType: mediaType, ArtworkURL: '', ArtworkKind: 'poster', Status: status, Version: 1,
    RequestedBy: 'user-1', Note: '', availability: 'unknown', Platforms: { any: true }, CreatedAt: '2026-10-28T12:00:00Z', subscribed: true, subscriptionSource: 'requester', ...over,
})

async function openHome(page: Page, requests: (url: URL) => { status?: number; json: unknown }) {
    const calls: string[] = []
    await page.route('**/api/v1/**', route => {
        const url = new URL(route.request().url())
        const path = url.pathname.replace('/api/v1', '')
        if (path === '/setup/status') return route.fulfill({ json: { complete: true, required: false, wizardComplete: true } })
        if (path === '/auth/me') return route.fulfill({ json: { user: { id: 'user-1', role: 'requester', displayName: 'User' }, csrfToken: 'csrf-token' } })
        if (path === '/requests') { calls.push(url.search); return route.fulfill(requests(url)) }
        return route.fulfill({ status: 404, json: { error: { code: 'not_found', message: 'unmocked' } } })
    })
    await page.goto('/')
    return calls
}

test('Home lists the five newest requests the user follows, with status and library state', async ({ page }) => {
    const calls = await openHome(page, () => ({ json: { items: [row('1', 'Arrival', 'movie', 'requested'), row('2', 'Severance', 'tv', 'available', { availability: 'available' }), row('3', 'Kid A', 'album', 'approved', { ArtworkURL: '/art/kid-a.png', ArtworkKind: 'square' })], nextCursor: null } }))
    const section = page.getByRole('region', { name: 'Your recent requests' })
    await expect(section.getByRole('link', { name: 'Arrival' })).toHaveAttribute('href', '/requests?requestId=1')
    await expect(section.getByRole('listitem')).toHaveCount(3)
    await expect(section.getByRole('listitem').filter({ hasText: 'Severance' })).toContainText('available')
    await expect(section.getByRole('img', { name: 'Artwork for Kid A' })).toBeVisible()
    await expect(section.getByRole('link', { name: 'Open the full queue' })).toHaveAttribute('href', '/requests?mine=true')
    // The dev server runs effects twice; the query is what matters.
    expect([...new Set(calls)].filter(call => call.includes('mine'))).toEqual(['?mine=true&limit=5'])
})

test('Home says so when nothing has been requested yet', async ({ page }) => {
    await openHome(page, () => ({ json: { items: [], nextCursor: null } }))
    await expect(page.getByText('Nothing yet. Paste a link above to make your first request.')).toBeVisible()
})

test('a failing list never blocks requesting', async ({ page }) => {
    await openHome(page, () => ({ status: 500, json: { error: { code: 'requests_list_failed', message: 'could not list requests' } } }))
    await expect(page.getByText('Your recent requests could not be loaded.')).toBeVisible()
    await expect(page.getByPlaceholder('Paste a supported link…')).toBeEnabled()
    await expect(page.getByRole('button', { name: 'Preview', exact: true })).toBeEnabled()
})

test('Home adds a Recently available row from shared requests, and omits it when empty', async ({ page }) => {
    const calls = await openHome(page, url => ({ json: { items: url.searchParams.get('status') === 'available' ? [row('9', 'Arrival', 'movie', 'available')] : [], nextCursor: null } }))
    const available = page.getByRole('region', { name: 'Recently available' })
    await expect(available.getByRole('link', { name: /Arrival/ })).toHaveAttribute('href', '/requests?requestId=9')
    await expect(available.getByRole('link', { name: 'All available' })).toHaveAttribute('href', '/requests?status=available')
    expect([...new Set(calls)]).toContain('?status=available&limit=6')
})

test('Home leaves the Recently available row out when there is nothing to show', async ({ page }) => {
    await openHome(page, () => ({ json: { items: [], nextCursor: null } }))
    await expect(page.getByText('Nothing yet. Paste a link above to make your first request.')).toBeVisible()
    await expect(page.getByRole('region', { name: 'Recently available' })).toHaveCount(0)
})
