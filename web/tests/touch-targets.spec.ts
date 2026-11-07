import { expect, test, type Locator, type Page } from '@playwright/test'
import { sessionFixture } from './capabilities'

// At phone width every control the four roles added is at least 44px high, so a
// thumb can hit it. The request-on-behalf picker is checked here too.
test.use({ viewport: { width: 390, height: 844 } })

const auth = { state: 'not_configured', missing: [], ignored: [], differing: [], saved: null, effective: { enabled: false, providerName: '', issuer: '', clientId: '', scopes: 'openid profile email groups', assignRolesFromGroups: true, groupsClaim: 'groups', ownerGroup: 'owner', adminGroup: 'admin', userGroup: 'user', viewerGroup: 'viewer' }, secretConfigured: false, secretUnreadable: false, canSaveSecret: true, readOnly: false, redirectUri: 'https://requestarr.example.test/api/v1/auth/callback', working: false, problem: '' }
const users = [
    { id: 'u1', email: 'me@example.test', displayName: 'Me', role: 'superuser', canRequest: true, builtin: false, roleChoices: [], createdAt: '2026-01-01T00:00:00Z' },
    { id: 'u2', email: 'vic@example.test', displayName: 'Vic', role: 'viewer', canRequest: false, builtin: false, roleChoices: ['viewer', 'requester', 'admin', 'superuser'], createdAt: '2026-01-01T00:00:00Z' },
]

async function openSettings(page: Page, url: string) {
    await page.route('**/api/v1/**', route => {
        const path = new URL(route.request().url()).pathname.replace('/api/v1', '')
        if (path === '/auth/me') return route.fulfill({ json: sessionFixture('superuser') })
        if (path === '/admin/settings/authentication') return route.fulfill({ json: auth })
        if (path === '/admin/settings/authentication/local-administrator') return route.fulfill({ json: { exists: true, disabled: false, canDisable: false } })
        if (path === '/admin/users') return route.fulfill({ json: { users, rolesFromGroups: false } })
        if (path === '/admin/settings/people') return route.fulfill({ json: { defaultRole: 'viewer' } })
        if (path === '/admin/settings/approvals') return route.fulfill({ json: { autoApproveTypes: [] } })
        if (path === '/admin/checklist') return route.fulfill({ json: { visible: true, items: [] } })
        if (path === '/admin/secrets') return route.fulfill({ json: { enabled: true, secrets: [] } })
        if (path === '/admin/library-connections') return route.fulfill({ json: { connections: [] } })
        if (path === '/admin/status') return route.fulfill({ json: { providers: [], sources: [], recentErrors: [] } })
        if (path === '/admin/job-runs') return route.fulfill({ json: { sources: [] } })
        if (path === '/admin/match-suggestions') return route.fulfill({ json: { items: [] } })
        return route.fulfill({ json: { configured: false, enabled: false, items: [], unread: 0, awaitingApproval: 0, open: 0 } })
    })
    await page.goto(url)
    await expect(page.getByRole('heading', { name: 'Settings' })).toBeVisible()
}

async function height(target: Locator) {
    await expect(target).toBeVisible()
    const box = await target.boundingBox()
    return box?.height ?? 0
}

test('the role selects on the Users tab are at least 44px high', async ({ page }) => {
    await openSettings(page, '/settings?tab=users')
    const selects = page.locator('main select:visible')
    expect(await selects.count()).toBeGreaterThan(1)
    for (let i = 0; i < await selects.count(); i++) expect(await height(selects.nth(i))).toBeGreaterThanOrEqual(44)
})

test('the Enable switch, the group fields and the scopes field are at least 44px high', async ({ page }) => {
    await openSettings(page, '/settings?tab=authentication')
    const targets = [
        page.getByRole('switch').locator('xpath=ancestor::label'),
        page.getByLabel('Scopes'),
        page.getByLabel('Assign roles from groups').locator('xpath=ancestor-or-self::label | self::input').first(),
        page.getByLabel('Group claim'),
        page.getByLabel('Superuser group'),
        page.getByLabel('Admin group'),
        page.getByLabel('Requester group'),
        page.getByLabel('Viewer group'),
    ]
    for (const target of targets) expect(await height(target)).toBeGreaterThanOrEqual(44)
})
