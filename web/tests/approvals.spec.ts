import { expect, test, type Page } from '@playwright/test'
import AxeBuilder from '@axe-core/playwright'
import { capabilitiesFor } from './capabilities'

const waiting = (id: string, title: string) => ({
    ID: id, CatalogItemID: 'c-' + id, Title: title, MediaType: 'movie', ArtworkURL: '', ArtworkKind: 'poster', Status: 'requested', Version: 1,
    RequestedBy: 'u1', Note: '', Platforms: { any: true, platformKeys: [] }, CatalogPlatforms: [], CreatedAt: '2026-01-01T00:00:00Z',
    availability: 'unknown', subscribed: true, subscriptionSource: 'requester',
})

async function openApp(page: Page, role: string, count = 3) {
    const state = { count, summaryCalls: 0, listQueries: [] as string[], puts: [] as { csrf?: string; body: unknown }[], types: [] as string[] }
    await page.route('**/api/v1/**', route => {
        const request = route.request(), url = new URL(request.url()), path = url.pathname.replace('/api/v1', '')
        if (path === '/auth/me') return route.fulfill({ json: { user: { id: 'u1', role, displayName: 'Person' }, capabilities: capabilitiesFor(role), csrfToken: 'csrf' } })
        if (path === '/requests/summary') { state.summaryCalls++; return route.fulfill({ json: { requested: state.count } }) }
        if (path === '/requests') { state.listQueries.push(url.search); return route.fulfill({ json: { items: [waiting('r1', 'Arrival'), waiting('r2', 'Dune')], nextCursor: null } }) }
        if (path === '/requests/r1/transition') { state.count--; return route.fulfill({ json: { Request: { ...waiting('r1', 'Arrival'), Status: request.postDataJSON().toStatus, Version: 2 }, Changed: true } }) }
        if (path === '/admin/settings/approvals') {
            if (request.method() === 'PUT') { const body = request.postDataJSON(); state.puts.push({ csrf: request.headers()['x-csrf-token'], body }); state.types = body.autoApproveTypes; return route.fulfill({ json: body }) }
            return route.fulfill({ json: { autoApproveTypes: state.types } })
        }
        if (path === '/admin/settings/people') return route.fulfill({ json: { defaultRole: 'viewer' } })
        if (path === '/issues/summary') return route.fulfill({ json: { open: 0 } })
        if (path === '/admin/users') return route.fulfill({ json: { users: [] } })
        if (path === '/notifications') return route.fulfill({ json: { items: [] } })
        if (path === '/admin/secrets') return route.fulfill({ json: { enabled: true, secrets: [] } })
        if (path === '/admin/library-connections') return route.fulfill({ json: { connections: [] } })
        if (path === '/admin/status') return route.fulfill({ json: { providers: [], sources: [], recentErrors: [] } })
        if (path === '/admin/job-runs') return route.fulfill({ json: { sources: [] } })
        if (path === '/admin/match-suggestions') return route.fulfill({ json: { items: [] } })
        return route.fulfill({ json: { configured: false, enabled: false, items: [], nextCursor: null } })
    })
    return state
}

test('the admin nav counts requests waiting and opens them filtered', async ({ page }) => {
    await openApp(page, 'admin', 3)
    await page.goto('/library')
    const link = page.getByRole('navigation', { name: 'Main navigation' }).getByRole('link', { name: /^Requests/ })
    await expect(link).toContainText('3')
    await expect(link.getByLabel('3 waiting for approval')).toBeVisible()
    await link.click()
    await expect(page).toHaveURL(/\/requests\?status=requested$/)
    await expect(page.getByRole('button', { name: 'Requested', exact: true })).toHaveAttribute('aria-pressed', 'true')
})

test('the count drops after approving or denying without a reload', async ({ page }) => {
    const state = await openApp(page, 'admin', 2)
    await page.goto('/requests?status=requested')
    const link = page.getByRole('navigation', { name: 'Main navigation' }).getByRole('link', { name: /^Requests/ })
    await expect(link).toContainText('2')
    await page.getByRole('button', { name: 'Approve', exact: true }).first().click()
    await expect(link).toContainText('1')
    expect(state.count).toBe(1)
})

test('no count is shown, or even fetched, for a requester or a viewer', async ({ page }) => {
    for (const role of ['requester', 'viewer']) {
        const state = await openApp(page, role, 7)
        await page.goto('/library')
        await expect(page.getByRole('navigation', { name: 'Main navigation' }).getByRole('link', { name: 'Requests', exact: true })).toBeVisible()
        await expect(page.getByLabel(/waiting for approval/)).toHaveCount(0)
        expect(state.summaryCalls).toBe(0)
        await page.unroute('**/api/v1/**')
    }
})

test('an administrator chooses which media types are approved automatically', async ({ page }) => {
    const state = await openApp(page, 'admin')
    await page.goto('/settings?tab=users')
    const group = page.getByRole('group', { name: 'Approve these automatically' })
    await expect(group.getByRole('checkbox')).toHaveCount(6)
    for (const label of ['Movie', 'TV', 'Album', 'Song', 'Game', 'Audiobook']) await expect(group.getByRole('checkbox', { name: label, exact: true })).not.toBeChecked()
    await group.getByRole('checkbox', { name: 'Movie', exact: true }).check()
    await expect(page.getByRole('status').filter({ hasText: 'Requests for Movie are approved automatically.' })).toBeVisible()
    await group.getByRole('checkbox', { name: 'Audiobook', exact: true }).check()
    expect(state.puts).toEqual([{ csrf: 'csrf', body: { autoApproveTypes: ['movie'] } }, { csrf: 'csrf', body: { autoApproveTypes: ['movie', 'audiobook'] } }])
    await group.getByRole('checkbox', { name: 'Movie', exact: true }).uncheck()
    await expect(page.getByRole('status').filter({ hasText: 'Requests for Audiobook are approved automatically.' })).toBeVisible()
    await page.reload()
    await expect(page.getByRole('group', { name: 'Approve these automatically' }).getByRole('checkbox', { name: 'Audiobook', exact: true })).toBeChecked()
    const results = await new AxeBuilder({ page }).analyze()
    expect(results.violations).toEqual([])
})
