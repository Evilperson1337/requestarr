import AxeBuilder from '@axe-core/playwright'
import { expect, test, type Page } from '@playwright/test'

// The Users list among people: the local administrator is marked and its role is locked (D040).

async function openUsers(page: Page) {
    const puts: { path: string; body: unknown }[] = []
    await page.route('**/api/v1/**', route => {
        const request = route.request(), path = new URL(request.url()).pathname.replace('/api/v1', '')
        if (path === '/auth/me') return route.fulfill({ json: { user: { id: 'u2', role: 'admin', displayName: 'Ann' }, csrfToken: 'csrf' } })
        if (path === '/admin/users') return route.fulfill({ json: { users: [
            { id: 'u1', email: '', displayName: 'Administrator', role: 'admin', builtin: true, createdAt: '2026-10-01T00:00:00Z' },
            { id: 'u2', email: 'ann@example.test', displayName: 'Ann', role: 'admin', builtin: false, createdAt: '2026-10-02T00:00:00Z' },
            { id: 'u3', email: 'bob@example.test', displayName: 'Bob', role: 'viewer', builtin: false, createdAt: '2026-10-03T00:00:00Z' },
        ] } })
        if (path.startsWith('/admin/users/') && request.method() === 'PUT') { puts.push({ path, body: request.postDataJSON() }); return route.fulfill({ status: 204 }) }
        if (path === '/admin/settings/people') return route.fulfill({ json: { defaultRole: 'viewer' } })
        if (path === '/admin/settings/approvals') return route.fulfill({ json: { autoApproveTypes: [] } })
        if (path === '/admin/secrets') return route.fulfill({ json: { enabled: true, secrets: [] } })
        if (path === '/admin/library-connections') return route.fulfill({ json: { connections: [] } })
        if (path === '/admin/status') return route.fulfill({ json: { providers: [], sources: [], recentErrors: [] } })
        if (path === '/admin/job-runs') return route.fulfill({ json: { sources: [] } })
        if (path === '/admin/match-suggestions') return route.fulfill({ json: { items: [] } })
        if (path === '/notifications') return route.fulfill({ json: { items: [] } })
        return route.fulfill({ json: { configured: false, enabled: false } })
    })
    await page.goto('/settings?tab=users')
    await expect(page.getByRole('region', { name: 'Users' })).toBeVisible()
    return puts
}

test('the Users list marks the local administrator and locks its role', async ({ page }) => {
    await openUsers(page)
    const row = page.getByRole('row').filter({ hasText: 'Administrator' })
    await expect(row.getByText('Local administrator')).toBeVisible()
    await expect(row.getByRole('combobox')).toHaveCount(0)
    await expect(row).toContainText('admin')
    await expect(row).toContainText('Always administrator')
})

test('another person is still promoted from the list', async ({ page }) => {
    const puts = await openUsers(page)
    await page.getByLabel('Role for Bob').selectOption('admin')
    await expect.poll(() => puts).toEqual([{ path: '/admin/users/u3/role', body: { role: 'admin' } }])
})

test('the Users list passes the accessibility audit', async ({ page }) => {
    await openUsers(page)
    const results = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze()
    expect(results.violations.map(v => v.id)).toEqual([])
})
