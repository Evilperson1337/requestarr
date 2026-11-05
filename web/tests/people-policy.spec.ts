import { expect, test, type Page } from '@playwright/test'

async function openApp(page: Page, role = 'admin', notifications: unknown[] = []) {
    const puts: { csrf?: string; body: unknown }[] = []
    let defaultRole = 'viewer'
    await page.route('**/api/v1/**', route => {
        const request = route.request(), path = new URL(request.url()).pathname.replace('/api/v1', '')
        if (path === '/auth/me') return route.fulfill({ json: { user: { id: 'u1', role, displayName: 'Admin' }, csrfToken: 'csrf' } })
        if (path === '/admin/settings/people') {
            if (request.method() === 'PUT') { const body = request.postDataJSON(); puts.push({ csrf: request.headers()['x-csrf-token'], body }); defaultRole = body.defaultRole; return route.fulfill({ json: body }) }
            return route.fulfill({ json: { defaultRole } })
        }
        if (path === '/admin/users') return route.fulfill({ json: { users: [] } })
        if (path === '/notifications') return route.fulfill({ json: { items: notifications } })
        if (path === '/admin/secrets') return route.fulfill({ json: { enabled: true, secrets: [] } })
        if (path === '/admin/library-connections') return route.fulfill({ json: { connections: [] } })
        if (path === '/admin/status') return route.fulfill({ json: { providers: [], sources: [], recentErrors: [] } })
        if (path === '/admin/job-runs') return route.fulfill({ json: { sources: [] } })
        if (path === '/admin/match-suggestions') return route.fulfill({ json: { items: [] } })
        return route.fulfill({ json: { configured: false, enabled: false } })
    })
    return puts
}

test('an administrator chooses the role new people get', async ({ page }) => {
    const puts = await openApp(page)
    await page.goto('/settings?tab=users')
    const select = page.getByLabel('Role for people who sign in for the first time')
    await expect(select).toBeVisible()
    await expect(select).toHaveValue('viewer')
    await expect(select.locator('option')).toHaveText(['viewer', 'requester'])
    await select.selectOption('requester')
    await expect(page.getByRole('status').filter({ hasText: 'New people will start as requester.' })).toBeVisible()
    expect(puts).toEqual([{ csrf: 'csrf', body: { defaultRole: 'requester' } }])
})

test('a first sign-in notice links to Settings, Users', async ({ page }) => {
    await openApp(page, 'admin', [{ id: 'n1', title: 'Ann signed in for the first time as viewer', body: 'Open Settings, Users to change their role.', link: '/settings?tab=users', createdAt: '2026-10-01T12:00:00Z', readAt: null }])
    await page.goto('/notifications')
    await page.getByRole('link', { name: 'Open' }).click()
    await expect(page).toHaveURL(/\/settings\?tab=users$/)
    await expect(page.getByLabel('Role for people who sign in for the first time')).toBeVisible()
})
