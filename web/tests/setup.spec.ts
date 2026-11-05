import { expect, test, type Page } from '@playwright/test'

// First-admin claim (D037) and the Get started checklist that follows it (D038).

type Item = { key: string; title: string; required: boolean; done: boolean; detail?: string }
const fresh: Item[] = [
    { key: 'libraries', title: 'Connect a media library', required: true, done: false, detail: 'Connect Jellyfin, Plex, Navidrome, RomM or Audiobookshelf so Requestarr can tell what you already have.' },
    { key: 'metadata', title: 'Add a metadata provider', required: true, done: false, detail: 'A TMDB read token turns on Movies and TV search; IGDB turns on Games. Music works without setup.' },
    { key: 'notifications', title: 'Set up notifications', required: false, done: false, detail: 'Optional. Without Apprise, people only see availability inside Requestarr.' },
    { key: 'people', title: 'Review who can do what', required: false, done: false, detail: 'Optional. New people start as viewers and every request waits for approval until you change the default role or the approval policy.' },
]

async function mockChecklist(page: Page, options: { role?: string; items?: Item[]; visible?: boolean; dismissed?: boolean } = {}) {
    const state = {
        calls: [] as { method: string; path: string; csrf?: string }[],
        items: options.items ?? fresh, visible: options.visible ?? true, dismissed: options.dismissed ?? false,
    }
    await page.route('**/api/v1/**', async route => {
        const request = route.request()
        const path = new URL(request.url()).pathname.replace('/api/v1', '')
        const method = request.method()
        if (path === '/setup/status') return route.fulfill({ json: { complete: true, required: false } })
        if (path === '/auth/me') return route.fulfill({ json: { user: { id: 'user-1', role: options.role ?? 'admin', email: 'admin@example.test', displayName: 'Admin' }, csrfToken: 'csrf-token' } })
        state.calls.push({ method, path, csrf: request.headers()['x-csrf-token'] })
        if (path === '/admin/checklist') return route.fulfill({ json: { items: state.items, dismissed: state.dismissed, visible: state.visible } })
        if (path === '/admin/checklist/dismiss') { state.dismissed = true; state.visible = false; return route.fulfill({ status: 204 }) }
        return route.fulfill({ json: { items: [], nextCursor: null } })
    })
    return state
}

test('a claimed first admin lands on the Get started checklist', async ({ page }) => {
    let claimed = false
    let claimBody: unknown
    const state = await mockChecklist(page)
    await page.route('**/api/v1/setup/status', route => route.fulfill({ json: claimed ? { complete: true, required: false } : { complete: false, required: true } }))
    await page.route('**/api/v1/auth/me', route => route.fulfill({ json: { user: { id: 'user-1', role: claimed ? 'admin' : 'viewer', email: 'admin@example.test', displayName: 'Admin' }, csrfToken: 'csrf-token', ...(claimed ? {} : { firstAdminClaim: { issuer: 'https://identity.example.test', subject: 'bootstrap-identity', issuerIsConfigured: true } }) } }))
    await page.route('**/api/v1/setup/claim-admin', route => {
        expect(route.request().headers()['x-csrf-token']).toBe('csrf-token')
        claimBody = route.request().postDataJSON()
        claimed = true
        return route.fulfill({ status: 204 })
    })
    await page.goto('/')
    await expect(page.getByLabel('OIDC issuer')).toHaveCount(0)
    await expect(page.getByLabel('OIDC subject')).toHaveCount(0)
    await expect(page.getByTestId('setup-token-command')).toHaveText("docker exec <container-name> /requestarr setup-token --subject 'bootstrap-identity' --stdout")
    await page.getByLabel('One-time setup token').fill('one-time-bootstrap-fixture-token-123456789')
    await page.getByRole('button', { name: 'Claim first admin' }).click()
    await expect(page).toHaveURL('/get-started')
    expect(claimBody).toEqual({ token: 'one-time-bootstrap-fixture-token-123456789' })
    await expect(page.getByRole('heading', { name: 'Get started', level: 1 })).toBeVisible()
    await expect(page.getByRole('link', { name: 'Get started' })).toHaveAttribute('aria-current', 'page')
    expect(state.calls.some(call => call.path.startsWith('/admin/setup'))).toBe(false)
    expect(await page.evaluate(() => ({ local: { ...localStorage }, session: { ...sessionStorage } }))).toEqual({ local: {}, session: {} })
})

test('claim page adds --issuer to the command when the sign-in issuer is not the configured one, and offers a copy control', async ({ page, context }) => {
    await context.grantPermissions(['clipboard-read', 'clipboard-write'])
    await page.route('**/api/v1/setup/status', route => route.fulfill({ json: { complete: false, required: true } }))
    await page.route('**/api/v1/auth/me', route => route.fulfill({ json: { user: { id: 'user-1', role: 'viewer', email: 'a@example.test', displayName: 'A' }, csrfToken: 'csrf-token', firstAdminClaim: { issuer: 'https://other.example.test', subject: "o'brien", issuerIsConfigured: false } } }))
    await page.goto('/')
    const expected = "docker exec <container-name> /requestarr setup-token --issuer 'https://other.example.test' --subject 'o'\\''brien' --stdout"
    await expect(page.getByTestId('setup-token-command')).toHaveText(expected)
    await page.getByRole('button', { name: 'Copy command' }).click()
    expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(expected)
})

test('claim page without an identity from the server shows no command and no identity fields', async ({ page }) => {
    await page.route('**/api/v1/setup/status', route => route.fulfill({ json: { complete: false, required: true } }))
    await page.route('**/api/v1/auth/me', route => route.fulfill({ json: { user: { id: 'user-1', role: 'viewer', email: 'a@example.test', displayName: 'A' }, csrfToken: 'csrf-token' } }))
    await page.goto('/')
    await expect(page.getByTestId('setup-token-command')).toHaveCount(0)
    await expect(page.getByLabel('OIDC subject')).toHaveCount(0)
    await expect(page.getByLabel('One-time setup token')).toBeVisible()
})

test('the checklist separates required from optional items, shows live state, and asks for nothing itself', async ({ page }) => {
    await mockChecklist(page, { items: fresh.map(item => item.key === 'metadata' ? { ...item, done: true, detail: undefined } : item) })
    await page.goto('/get-started')
    const required = page.getByRole('region', { name: 'Required' })
    const optional = page.getByRole('region', { name: 'Optional' })
    await expect(required.getByRole('listitem')).toHaveCount(2)
    await expect(optional.getByRole('listitem')).toHaveCount(2)
    await expect(required.getByRole('listitem').filter({ hasText: 'Connect a media library' })).toContainText('To do')
    await expect(required.getByRole('listitem').filter({ hasText: 'Add a metadata provider' })).toContainText('Done')
    await expect(optional.getByRole('listitem').filter({ hasText: 'Set up notifications' })).toContainText('Optional. Without Apprise')
    // No value is entered twice: this page has no fields, only links and a dismissal.
    await expect(page.locator('main input, main select, main textarea')).toHaveCount(0)
})

test('each item links to the Settings section that applies it', async ({ page }) => {
    await mockChecklist(page)
    await page.goto('/get-started')
    const hrefs = async (title: string) => page.getByRole('listitem').filter({ hasText: title }).getByRole('link').getAttribute('href')
    expect(await hrefs('Connect a media library')).toBe('/settings?tab=integrations#group-libraries')
    expect(await hrefs('Add a metadata provider')).toBe('/settings?tab=integrations#group-metadata')
    expect(await hrefs('Set up notifications')).toBe('/settings?tab=integrations#group-notifications')
    expect(await hrefs('Review who can do what')).toBe('/settings?tab=users')
    await page.getByRole('listitem').filter({ hasText: 'Review who can do what' }).getByRole('link').click()
    await expect(page).toHaveURL('/settings?tab=users')
})

test('dismissing is remembered: it posts once with the CSRF token, leaves the page and removes the nav item', async ({ page }) => {
    const state = await mockChecklist(page)
    await page.goto('/get-started')
    await expect(page.getByRole('navigation', { name: 'Main navigation' }).getByRole('link', { name: 'Get started' })).toBeVisible()
    await page.getByRole('button', { name: 'Dismiss checklist' }).click()
    await expect(page).toHaveURL('/')
    await expect(page.getByRole('navigation', { name: 'Main navigation' }).getByRole('link', { name: 'Get started' })).toHaveCount(0)
    expect(state.calls.filter(call => call.path === '/admin/checklist/dismiss')).toEqual([{ method: 'POST', path: '/admin/checklist/dismiss', csrf: 'csrf-token' }])
    await page.reload()
    await expect(page.getByRole('navigation', { name: 'Main navigation' }).getByRole('link', { name: 'Get started' })).toHaveCount(0)
})

test('the nav item disappears once every required item is done', async ({ page }) => {
    await mockChecklist(page, { visible: false, items: fresh.map(item => item.required ? { ...item, done: true, detail: undefined } : item) })
    await page.goto('/')
    await expect(page.getByRole('link', { name: 'Settings' })).toBeVisible()
    await expect(page.getByRole('link', { name: 'Get started' })).toHaveCount(0)
})

for (const role of ['viewer', 'requester']) {
    test(`a ${role} never sees the checklist, its nav item or asks for it`, async ({ page }) => {
        const state = await mockChecklist(page, { role })
        await page.goto('/')
        await expect(page.getByRole('navigation', { name: 'Main navigation' })).toBeVisible()
        await expect(page.getByRole('link', { name: 'Get started' })).toHaveCount(0)
        await page.goto('/get-started')
        await expect(page.getByRole('heading', { name: 'Administrator access required' })).toBeVisible()
        expect(state.calls.some(call => call.path.startsWith('/admin/checklist'))).toBe(false)
    })
}

test('an old /setup bookmark no longer opens a draft wizard', async ({ page }) => {
    const state = await mockChecklist(page)
    await page.goto('/setup')
    await expect(page.getByRole('heading', { name: /Inactive draft/ })).toHaveCount(0)
    expect(state.calls.some(call => call.path.startsWith('/admin/setup'))).toBe(false)
})

test('a checklist that cannot load says so and offers a retry', async ({ page }) => {
    let fail = true
    await page.route('**/api/v1/**', route => {
        const path = new URL(route.request().url()).pathname.replace('/api/v1', '')
        if (path === '/setup/status') return route.fulfill({ json: { complete: true, required: false } })
        if (path === '/auth/me') return route.fulfill({ json: { user: { id: 'user-1', role: 'admin', email: 'a@example.test', displayName: 'Admin' }, csrfToken: 'csrf-token' } })
        if (path === '/admin/checklist') return fail ? route.fulfill({ status: 500, json: { error: { code: 'checklist_failed', message: 'could not load the Get started checklist' } } }) : route.fulfill({ json: { items: fresh, dismissed: false, visible: true } })
        return route.fulfill({ json: {} })
    })
    await page.goto('/get-started')
    await expect(page.getByRole('alert')).toContainText('could not load')
    fail = false
    await page.getByRole('button', { name: 'Try again' }).click()
    await expect(page.getByRole('region', { name: 'Required' })).toBeVisible()
})
