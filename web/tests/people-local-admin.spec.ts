import AxeBuilder from '@axe-core/playwright'
import { expect, test, type Page } from '@playwright/test'
import { capabilitiesFor } from './capabilities'

// The Users list among people: the local administrator is marked and its role is
// locked (D040), superusers and admins are labelled, and each person offers only
// the choices the signed-in role may make (the server decides, in roleChoices).

const all = ['viewer', 'requester', 'admin', 'superuser']
const lower = ['viewer', 'requester']

async function openUsers(page: Page, signedInAs: 'admin' | 'superuser') {
    const puts: { path: string; body: unknown }[] = []
    const owner = signedInAs === 'superuser'
    await page.route('**/api/v1/**', route => {
        const request = route.request(), path = new URL(request.url()).pathname.replace('/api/v1', '')
        if (path === '/auth/me') return route.fulfill({ json: { user: { id: 'u2', role: signedInAs, displayName: 'Ann' }, capabilities: capabilitiesFor(signedInAs), csrfToken: 'csrf' } })
        if (path === '/admin/users') return route.fulfill({ json: { users: [
            { id: 'u1', email: '', displayName: 'Administrator', role: 'superuser', builtin: true, createdAt: '2026-10-01T00:00:00Z', roleChoices: [] },
            { id: 'u2', email: 'ann@example.test', displayName: 'Ann', role: signedInAs, builtin: false, createdAt: '2026-10-02T00:00:00Z', roleChoices: [] },
            { id: 'u3', email: 'bob@example.test', displayName: 'Bob', role: 'viewer', builtin: false, createdAt: '2026-10-03T00:00:00Z', roleChoices: owner ? all : lower },
            { id: 'u4', email: 'cy@example.test', displayName: 'Cy', role: 'admin', builtin: false, createdAt: '2026-10-04T00:00:00Z', roleChoices: owner ? all : [] },
            { id: 'u5', email: 'di@example.test', displayName: 'Di', role: 'superuser', builtin: false, createdAt: '2026-10-05T00:00:00Z', roleChoices: owner ? all : [] },
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

const options = async (page: Page, name: string) => page.getByLabel(`Role for ${name}`).locator('option').allTextContents()

test('the Users list marks the local administrator and locks its role', async ({ page }) => {
    await openUsers(page, 'superuser')
    const row = page.getByRole('row').filter({ hasText: 'Local administrator' })
    await expect(row.getByRole('combobox')).toHaveCount(0)
    await expect(row.locator('.badge', { hasText: 'Superuser' })).toBeVisible()
    await expect(row).toContainText('Always a superuser')
})

test('a superuser offers all four roles and promotes from the list', async ({ page }) => {
    const puts = await openUsers(page, 'superuser')
    expect(await options(page, 'Bob')).toEqual(all)
    expect(await options(page, 'Cy')).toEqual(all)
    await page.getByLabel('Role for Bob').selectOption('admin')
    await expect.poll(() => puts).toEqual([{ path: '/admin/users/u3/role', body: { role: 'admin' } }])
})

test('an admin offers only requester and viewer and cannot touch an admin or a superuser', async ({ page }) => {
    const puts = await openUsers(page, 'admin')
    expect(await options(page, 'Bob')).toEqual(lower)
    for (const name of ['Cy', 'Di']) {
        const row = page.getByRole('row').filter({ hasText: name })
        await expect(row.getByRole('combobox')).toHaveCount(0)
        await expect(row).toContainText('Only a superuser can change this role')
    }
    await expect(page.getByRole('row').filter({ hasText: 'Cy' }).locator('.badge', { hasText: 'Admin' })).toBeVisible()
    await expect(page.getByRole('row').filter({ hasText: 'Di' }).locator('.badge', { hasText: 'Superuser' })).toBeVisible()
    await expect(page.getByRole('row').filter({ hasText: 'Ann' })).toContainText('Current user')
    await page.getByLabel('Role for Bob').selectOption('requester')
    await expect.poll(() => puts).toEqual([{ path: '/admin/users/u3/role', body: { role: 'requester' } }])
})

for (const [name, size] of [['desktop', { width: 1280, height: 900 }], ['phone', { width: 390, height: 844 }]] as const) {
    for (const role of ['admin', 'superuser'] as const) {
        test(`the Users list passes the accessibility audit as ${role} at ${name} width`, async ({ page }) => {
            await page.setViewportSize(size)
            await openUsers(page, role)
            const results = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze()
            expect(results.violations.map(v => v.id)).toEqual([])
        })
    }
}
