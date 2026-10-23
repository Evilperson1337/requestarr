import { expect, test, type Page, type Route } from '@playwright/test'

function request(overrides = {}) {
    return {
        ID: '22222222-2222-2222-2222-222222222222', CatalogItemID: 'catalog-1', Title: 'Arrival',
        MediaType: 'movie', ArtworkURL: '', ArtworkKind: 'poster', Status: 'requested', Version: 3,
        RequestedBy: 'user-1', Note: '', Platforms: { any: true, platformKeys: [] }, CatalogPlatforms: [],
        CreatedAt: '2026-01-01T00:00:00Z', availability: 'unknown', subscribed: true, subscriptionSource: 'requester',
        ...overrides,
    }
}

async function mockApp(page: Page, handler: (route: Route, url: URL) => Promise<void>, role = 'requester') {
    await page.route('**/api/v1/**', async route => {
        const url = new URL(route.request().url())
        if (url.pathname === '/api/v1/setup/status') return route.fulfill({ json: { complete: true, required: false } })
        if (url.pathname === '/api/v1/auth/me') return route.fulfill({ json: { user: { id: 'user-1', role, email: 'user@example.test', displayName: 'User' }, csrfToken: 'csrf-token' } })
        return handler(route, url)
    })
}

test('bookmark opens Requests, reflects all controls, and survives reload', async ({ page }) => {
    const calls: URL[] = []
    await mockApp(page, async (route, url) => {
        calls.push(url)
        await route.fulfill({ json: { items: [request()], nextCursor: null } })
    })
    const query = '?q=arrival&status=requested&mediaType=movie&mine=true&sort=oldest&limit=25'
    await page.goto('/requests' + query)
    await expect(page.getByRole('heading', { name: 'Requests', exact: true })).toBeVisible()
    await expect(page.getByRole('searchbox', { name: 'Search titles' })).toHaveValue('arrival')
    await expect(page.getByRole('button', { name: 'Requested', exact: true })).toHaveAttribute('aria-pressed', 'true')
    await expect(page.getByLabel('Media type')).toHaveValue('movie')
    await expect(page.getByLabel('Sort by')).toHaveValue('oldest')
    await expect(page.getByLabel('Requests per page')).toHaveValue('25')
    await expect(page.getByRole('checkbox', { name: 'My Requests' })).toBeChecked()
    expect(calls.at(-1)?.searchParams.toString()).toBe(new URLSearchParams(query).toString())
    await page.reload()
    await expect(page.getByRole('searchbox', { name: 'Search titles' })).toHaveValue('arrival')
    await expect(page.getByRole('heading', { name: 'Arrival', exact: true })).toBeVisible()
})

test('paging and filter changes update URLs and browser history', async ({ page }) => {
    const calls: URL[] = []
    await mockApp(page, async (route, url) => {
        calls.push(url)
        const second = url.searchParams.has('cursor')
        await route.fulfill({ json: { items: [request({ Title: second ? 'Second item' : 'First item' })], nextCursor: second ? null : 'opaque+/=cursor' } })
    })
    await page.goto('/requests?mine=true&limit=25')
    await expect(page.getByRole('heading', { name: 'First item' })).toBeVisible()
    await page.getByRole('link', { name: 'Next page', exact: true }).click()
    await expect(page.getByRole('heading', { name: 'Second item' })).toBeVisible()
    await expect(page.getByRole('heading', { name: 'Requests', exact: true })).toBeFocused()
    expect(new URL(page.url()).searchParams.get('cursor')).toBe('opaque+/=cursor')
    expect(calls.at(-1)?.searchParams.get('mine')).toBe('true')
    await page.getByRole('link', { name: 'Previous page', exact: true }).click()
    await expect(page.getByRole('heading', { name: 'First item' })).toBeVisible()
    await page.goForward()
    await expect(page.getByRole('heading', { name: 'Second item' })).toBeVisible()
    await page.getByLabel('Sort by').selectOption('status')
    await expect(page.getByRole('heading', { name: 'First item' })).toBeVisible()
    expect(new URL(page.url()).searchParams.has('cursor')).toBe(false)
    expect(new URL(page.url()).searchParams.get('sort')).toBe('status')
    await page.getByRole('searchbox', { name: 'Search titles' }).fill('a title')
    await page.getByRole('searchbox', { name: 'Search titles' }).press('Enter')
    await expect(page).toHaveURL(/q=a\+title/)
    await page.getByRole('button', { name: 'Approved', exact: true }).click()
    await expect(page).toHaveURL(/status=approved/)
    await page.getByRole('checkbox', { name: 'My Requests' }).uncheck()
    await expect(page.getByRole('checkbox', { name: 'My Requests' })).not.toBeChecked()
    expect(new URL(page.url()).searchParams.has('mine')).toBe(false)
    await page.goBack()
    await expect(page.getByRole('checkbox', { name: 'My Requests' })).toBeChecked()
})

test('expired bookmarked cursor restarts while preserving filters', async ({ page }) => {
    await mockApp(page, async (route, url) => {
        if (url.searchParams.has('cursor')) return route.fulfill({ status: 410, json: { error: { code: 'cursor_expired', message: 'This cursor expired', requestId: 'expired-1' } } })
        await route.fulfill({ json: { items: [], nextCursor: null } })
    })
    await page.goto('/requests?q=arrival&mediaType=movie&cursor=expired')
    await expect(page.getByRole('alert')).toContainText('This cursor expired')
    await page.getByRole('button', { name: 'Restart from first page' }).click()
    await expect(page).not.toHaveURL(/cursor=/)
    expect(new URL(page.url()).searchParams.get('q')).toBe('arrival')
    expect(new URL(page.url()).searchParams.get('mediaType')).toBe('movie')
    await expect(page.getByRole('heading', { name: 'No matching requests' })).toBeVisible()
})

for (const role of ['viewer', 'requester', 'admin']) {
    test(`${role} sees only permitted cancel and subscription actions`, async ({ page }) => {
        const items = [
            request({ Title: 'Owned' }),
            request({ ID: 'followed', Title: 'Followed', RequestedBy: 'other', subscriptionSource: 'manual' }),
            request({ ID: 'unfollowed', Title: 'Unfollowed', RequestedBy: 'other', subscribed: false, subscriptionSource: '' }),
            request({ ID: 'closed', Title: 'Closed', Status: 'available' }),
            request({ ID: 'closed-unfollowed', Title: 'Closed unfollowed', Status: 'denied', subscribed: false, subscriptionSource: '' }),
        ]
        await mockApp(page, async route => { await route.fulfill({ json: { items, nextCursor: null } }) }, role)
        await page.goto('/requests')
        const row = (title: string) => page.getByRole('listitem').filter({ has: page.getByRole('heading', { name: title, exact: true }) })
        await expect(row('Owned').getByRole('button', { name: 'Leave request', exact: true })).toBeVisible()
        await expect(row('Followed').getByRole('button', { name: 'Leave request', exact: true })).toBeVisible()
        await expect(row('Unfollowed').getByRole('button', { name: 'Notify Me', exact: true })).toBeVisible()
        await expect(row('Closed').getByRole('button', { name: 'Leave request', exact: true })).toBeVisible()
        await expect(row('Closed unfollowed').getByRole('button', { name: /Notify Me|Leave request/ })).toHaveCount(0)
        await expect(row('Owned').getByRole('button', { name: 'Cancel request', exact: true })).toHaveCount(role === 'admin' ? 1 : 0)
        await expect(row('Closed').getByRole('button', { name: 'Cancel request', exact: true })).toHaveCount(0)
    })
}

test('admin cancel prevents duplicate clicks and preserves conflict errors', async ({ page }) => {
    let posts = 0
    let release!: () => void
    const gate = new Promise<void>(resolve => { release = resolve })
    await mockApp(page, async (route, url) => {
        if (url.pathname.endsWith('/transition')) {
            posts++
            expect(route.request().headers()['x-csrf-token']).toBe('csrf-token')
            expect(route.request().postDataJSON()).toMatchObject({ toStatus: 'cancelled', expectedVersion: 3, operationId: expect.any(String) })
            await gate
            return route.fulfill({ status: 409, json: { error: { code: 'version_conflict', message: 'Request changed; refresh and retry', requestId: 'conflict-1' } } })
        }
        await route.fulfill({ json: { items: [request()], nextCursor: null } })
    }, 'admin')
    await page.goto('/requests')
    page.once('dialog', dialog => dialog.accept())
    await page.getByRole('button', { name: 'Cancel request', exact: true }).click()
    await expect(page.getByRole('button', { name: 'Cancel request', exact: true })).toBeDisabled()
    release()
    await expect(page.getByRole('alert')).toContainText('Request changed; refresh and retry')
    await expect(page.getByRole('alert')).toContainText('conflict-1')
    await expect(page.getByRole('button', { name: 'Cancel request', exact: true })).toBeEnabled()
    expect(posts).toBe(1)
})

test('last-subscriber cancellation is confirmed by the server and can be declined', async ({ page }) => {
    const deletes: string[] = []
    await mockApp(page, async (route, url) => {
        if (route.request().method() === 'DELETE') {
            deletes.push(url.search)
            expect(route.request().headers()['x-csrf-token']).toBe('csrf-token')
            if (!url.searchParams.has('confirmCancel')) return route.fulfill({ status: 409, json: { error: { code: 'would_cancel', message: 'Would cancel', requestId: 'confirm-1' } } })
            return route.fulfill({ json: { subscribed: false, changed: true, cancelled: true } })
        }
        await route.fulfill({ json: { items: [request()], nextCursor: null } })
    })
    await page.goto('/requests')
    page.once('dialog', dialog => dialog.dismiss())
    await page.getByRole('button', { name: 'Leave request', exact: true }).click()
    await expect(page.getByRole('button', { name: 'Leave request', exact: true })).toBeEnabled()
    await expect(page.getByRole('alert')).toHaveCount(0)
    expect(deletes).toEqual([''])
    page.once('dialog', dialog => dialog.accept())
    await page.getByRole('button', { name: 'Leave request', exact: true }).click()
    await expect(page.getByRole('status').filter({ hasText: 'last subscriber' })).toBeVisible()
    expect(deletes).toEqual(['', '', '?confirmCancel=true'])
})

test('phone layout preserves artwork ratios and keyboard-operable controls', async ({ page }, testInfo) => {
    await page.setViewportSize({ width: 360, height: 800 })
    await page.route('**/art/*', route => {
        const height = route.request().url().endsWith('square') ? 200 : 300
        return route.fulfill({ contentType: 'image/svg+xml', body: `<svg xmlns="http://www.w3.org/2000/svg" width="200" height="${height}"><rect width="200" height="${height}" fill="#794cff"/></svg>` })
    })
    await mockApp(page, async route => {
        await route.fulfill({
            json: {
                items: [
                    request({ Title: 'LongTitle'.repeat(15), ArtworkURL: '/art/poster' }),
                    request({ ID: 'album', Title: 'Square Album', MediaType: 'album', ArtworkKind: 'square', ArtworkURL: '/art/square' }),
                ], nextCursor: null
            }
        })
    }, 'admin')
    await page.goto('/requests')
    const square = page.getByRole('img', { name: 'Artwork for Square Album' })
    await expect(square).toBeVisible()
    const posterBox = await page.getByRole('img').first().boundingBox()
    const squareBox = await square.boundingBox()
    expect(posterBox!.height / posterBox!.width).toBeGreaterThan(1.4)
    expect(Math.abs(squareBox!.height - squareBox!.width)).toBeLessThan(1)
    await expect(square).toHaveCSS('object-fit', 'contain')
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(360)
    const search = page.getByRole('searchbox', { name: 'Search titles' })
    await search.focus()
    await page.keyboard.press('Tab')
    await expect(page.getByRole('button', { name: 'Search', exact: true })).toBeFocused()
    await page.keyboard.press('Tab')
    await expect(page.getByRole('button', { name: 'All', exact: true })).toBeFocused()
    await page.keyboard.press('Tab')
    await expect(page.getByRole('button', { name: 'Requested', exact: true })).toBeFocused()
    await page.keyboard.press('Enter')
    await expect(page).toHaveURL(/status=requested/)
    await page.getByLabel('Media type').focus()
    await expect(page.getByLabel('Media type')).toHaveCSS('outline-style', 'solid')
    await expect(page.getByLabel('Media type')).toHaveCSS('outline-width', '3px')
    await page.screenshot({ path: testInfo.outputPath('phone-requests.png'), fullPage: true })
})

test('late responses cannot overwrite a newly selected view', async ({ page }) => {
    let release!: () => void
    const gate = new Promise<void>(resolve => { release = resolve })
    let delayed = false
    await mockApp(page, async (route, url) => {
        if (!url.searchParams.has('status')) {
            delayed = true
            await gate
            await route.fulfill({ json: { items: [request({ Title: 'Stale result' })], nextCursor: null } }).catch(() => { })
            return
        }
        await route.fulfill({ json: { items: [request({ Title: 'Current result' })], nextCursor: null } })
    })
    await page.goto('/requests')
    await expect.poll(() => delayed).toBe(true)
    await page.getByRole('button', { name: 'Approved', exact: true }).click()
    await expect(page.getByRole('heading', { name: 'Current result' })).toBeVisible()
    release()
    await expect(page.getByRole('heading', { name: 'Current result' })).toBeVisible()
    await expect(page.getByRole('heading', { name: 'Stale result' })).toHaveCount(0)
})

test('viewer can subscribe with CSRF and receives updated actions', async ({ page }) => {
    let subscribed = false
    await mockApp(page, async route => {
        if (route.request().method() === 'PUT') {
            expect(route.request().headers()['x-csrf-token']).toBe('csrf-token')
            subscribed = true
            return route.fulfill({ json: { subscribed: true, changed: true } })
        }
        await route.fulfill({ json: { items: [request({ subscribed, subscriptionSource: subscribed ? 'manual' : '', RequestedBy: 'other' })], nextCursor: null } })
    }, 'viewer')
    await page.goto('/requests')
    await page.getByRole('button', { name: 'Notify Me', exact: true }).click()
    await expect(page.getByRole('status').filter({ hasText: 'Notifications are on' })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Leave request', exact: true })).toBeEnabled()
})

test('Previous restores original first-page membership after inserts; Refresh starts a new view', async ({ page }) => {
    let inserted = false
    let firstPageReads = 0
    await mockApp(page, async (route, url) => {
        if (url.searchParams.has('cursor')) {
            inserted = true
            return route.fulfill({ json: { items: [request({ Title: 'C' }), request({ ID: 'd', Title: 'D' })], nextCursor: null } })
        }
        firstPageReads++
        await route.fulfill({
            json: {
                items: inserted
                    ? [request({ Title: 'New X' }), request({ ID: 'a', Title: 'A' })]
                    : [request({ Title: 'A' }), request({ ID: 'b', Title: 'B' })], nextCursor: 'snapshot-page-two'
            }
        })
    })
    await page.goto('/requests?limit=2')
    await expect(page.getByRole('heading', { name: 'B', exact: true })).toBeVisible()
    const originalReads = firstPageReads
    await page.getByRole('link', { name: 'Next page', exact: true }).click()
    await expect(page.getByRole('heading', { name: 'C', exact: true })).toBeVisible()
    await page.getByRole('link', { name: 'Previous page', exact: true }).click()
    await expect(page.getByRole('heading', { name: 'B', exact: true })).toBeVisible()
    await expect(page.getByRole('heading', { name: 'New X', exact: true })).toHaveCount(0)
    expect(firstPageReads).toBe(originalReads)
    await page.goForward()
    await expect(page.getByRole('heading', { name: 'C', exact: true })).toBeVisible()
    await page.goBack()
    await expect(page.getByRole('heading', { name: 'B', exact: true })).toBeVisible()
    await page.getByRole('button', { name: 'Refresh view' }).click()
    await expect(page.getByRole('heading', { name: 'New X', exact: true })).toBeVisible()
    expect(firstPageReads).toBe(originalReads + 1)
})
