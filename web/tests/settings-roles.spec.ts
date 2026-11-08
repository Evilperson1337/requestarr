import AxeBuilder from '@axe-core/playwright'
import { expect, test, type Page } from '@playwright/test'
import { sessionFixture } from './capabilities'

// Settings and the navigation show each role only what it can use: an admin has
// Activity, Matches and Users; Integrations, Authentication and Get started
// belong to the superuser and are absent (not disabled) for an admin.

type Role = 'admin' | 'superuser'

async function openSettings(page: Page, role: Role, url = '/settings') {
    const asked: string[] = []
    await page.route('**/api/v1/**', route => {
        const path = new URL(route.request().url()).pathname.replace('/api/v1', '')
        asked.push(path)
        if (path === '/auth/me') return route.fulfill({ json: sessionFixture(role, { id: 'u2', displayName: 'Ann' }) })
        if (path === '/admin/settings/authentication') return route.fulfill({ json: { state: 'not_configured', missing: [], ignored: [], differing: [], saved: null, effective: { enabled: false, providerName: '', issuer: '', clientId: '', scopes: 'openid profile email groups' }, secretConfigured: false, secretUnreadable: false, canSaveSecret: true, readOnly: false, redirectUri: 'https://requestarr.example.test/api/v1/auth/callback', working: false, problem: '' } })
        if (path === '/admin/settings/authentication/local-administrator') return route.fulfill({ json: { exists: true, disabled: false, canDisable: false } })
        if (path === '/admin/checklist') return route.fulfill({ json: { visible: true, items: [] } })
        if (path === '/admin/users') return route.fulfill({ json: { users: [] } })
        if (path === '/admin/settings/people') return route.fulfill({ json: { defaultRole: 'viewer' } })
        if (path === '/admin/settings/approvals') return route.fulfill({ json: { autoApproveTypes: [] } })
        if (path === '/admin/secrets') return route.fulfill({ json: { enabled: true, secrets: [] } })
        if (path === '/admin/library-connections') return route.fulfill({ json: { connections: [] } })
        if (path === '/admin/status') return route.fulfill({ json: { providers: [], sources: [], recentErrors: [] } })
        if (path === '/admin/job-runs') return route.fulfill({ json: { sources: [] } })
        if (path === '/admin/match-suggestions') return route.fulfill({ json: { items: [] } })
        if (path === '/notifications' || path === '/notifications/summary') return route.fulfill({ json: { items: [], unread: 0 } })
        if (path === '/requests/summary' || path === '/issues/count') return route.fulfill({ json: { awaitingApproval: 0, open: 0 } })
        return route.fulfill({ json: { configured: false, enabled: false } })
    })
    await page.goto(url)
    await expect(page.getByRole('heading', { name: 'Settings' })).toBeVisible()
    return asked
}

const tabNames = (page: Page) => page.getByRole('group', { name: 'Settings sections' }).getByRole('button').allTextContents()
const mainNav = (page: Page) => page.getByRole('navigation', { name: 'Main navigation' })

test('an admin sees Activity, Matches and Users and neither Integrations nor Authentication', async ({ page }) => {
    const asked = await openSettings(page, 'admin')
    expect(await tabNames(page)).toEqual(['Activity', 'Matches', 'Users'])
    await expect(page.getByRole('button', { name: 'Integrations' })).toHaveCount(0)
    await expect(page.getByRole('button', { name: 'Authentication' })).toHaveCount(0)
    await expect(page.getByRole('heading', { name: 'Media libraries' })).toHaveCount(0)
    await expect(page.getByRole('heading', { name: 'Recent sync runs' })).toBeVisible()
    // Nothing a superuser alone may read is asked for.
    expect(asked.filter(path => /secrets|library-connections|settings\/authentication|settings\/igdb|checklist/.test(path))).toEqual([])
})

test('an admin asking for the Integrations tab by link lands on Activity', async ({ page }) => {
    await openSettings(page, 'admin', '/settings?tab=authentication')
    await expect(page.getByRole('button', { name: 'Activity' })).toHaveAttribute('aria-pressed', 'true')
})

test('an admin has Settings in the navigation but no Get started entry', async ({ page }) => {
    await openSettings(page, 'admin')
    await expect(mainNav(page).getByRole('link', { name: 'Settings' })).toBeVisible()
    await expect(mainNav(page).getByRole('link', { name: 'Get started' })).toHaveCount(0)
})

test('an admin who opens Get started by address is told it is for the superuser', async ({ page }) => {
    await openSettings(page, 'admin')
    await page.goto('/get-started')
    await expect(page.getByRole('heading', { name: 'Superuser access required' })).toBeVisible()
})

test('a superuser sees every tab and Get started', async ({ page }) => {
    await openSettings(page, 'superuser')
    expect(await tabNames(page)).toEqual(['Integrations', 'Authentication', 'Activity', 'Matches', 'Users', 'Logs'])
    await expect(mainNav(page).getByRole('link', { name: 'Get started' })).toBeVisible()
    await expect(page.getByRole('heading', { name: 'Media libraries' })).toBeVisible()
})

for (const [name, size] of [['desktop', { width: 1280, height: 900 }], ['phone', { width: 390, height: 844 }]] as const) {
    for (const role of ['admin', 'superuser'] as const) {
        test(`Settings and the navigation pass the accessibility audit as ${role} at ${name} width`, async ({ page }) => {
            await page.setViewportSize(size)
            await openSettings(page, role)
            for (const tab of await tabNames(page)) {
                await page.getByRole('group', { name: 'Settings sections' }).getByRole('button', { name: tab }).click()
                const results = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze()
                expect(results.violations.map(v => v.id), tab).toEqual([])
            }
        })
    }
}
