import AxeBuilder from '@axe-core/playwright'
import { expect, test, type Page } from '@playwright/test'
import { capabilitiesFor } from './capabilities'

// Settings, Authentication (D040). Every response is a deterministic mock and every credential is synthetic.
const groupDefaults = { assignRolesFromGroups: false, groupsClaim: 'groups', ownerGroup: 'owner', adminGroup: 'admin', userGroup: 'user', viewerGroup: 'viewer' }
const saved = { enabled: false, providerName: 'Keycloak', issuer: 'https://identity.example.test/realms/home', clientId: 'requestarr', scopes: 'openid profile email', ...groupDefaults }
const empty = { enabled: false, providerName: '', issuer: '', clientId: '', scopes: 'openid profile email groups', ...groupDefaults }
const base = { state: 'not_configured', missing: [] as string[], ignored: [] as string[], differing: [] as string[], saved: null as typeof saved | null, effective: empty, secretConfigured: false, secretUnreadable: false, canSaveSecret: true, readOnly: false, redirectUri: 'https://requestarr.example.test/api/v1/auth/callback', working: false, problem: '' }
type State = typeof base
type Local = { exists: boolean; disabled: boolean; canDisable: boolean; otherAdminExists?: boolean; oidcWorking?: boolean }
const SECRET = 'synthetic-client-secret-for-test-only'
const GENERATED = 'Synthetic-Generated-Pass-0001'

const savedState = (over: Partial<typeof saved> = {}): Partial<State> => ({ state: 'saved_only', saved: { ...saved, ...over }, effective: { ...saved, ...over }, secretConfigured: true })

type Call = { path: string; method: string; body: Record<string, unknown> | null; csrf?: string }
async function setup(page: Page, patch: Partial<State> = {}, local: Local = { exists: true, disabled: false, canDisable: true }, localAdministrator = true) {
    const calls: Call[] = []
    const world = { auth: { ...base, ...patch } as State, local: { ...local } }
    page.on('request', req => {
        const path = new URL(req.url()).pathname.replace('/api/v1', '')
        if (path.startsWith('/admin/settings/authentication') || path === '/auth/local/password') calls.push({ path, method: req.method(), body: req.postData() ? (req.postDataJSON() as Record<string, unknown>) : null, csrf: req.headers()['x-csrf-token'] })
    })
    await page.route('**/api/v1/**', async route => {
        const req = route.request(), path = new URL(req.url()).pathname.replace('/api/v1', ''), method = req.method()
        if (path === '/auth/me') return route.fulfill({ json: { user: { id: 'admin-local', role: 'admin', displayName: 'Administrator', email: '' }, capabilities: capabilitiesFor('admin'), csrfToken: 'test-csrf', mustChangePassword: false, localAdministrator } })
        if (path.startsWith('/admin/settings/authentication') || path === '/auth/local/password') return route.fulfill({ status: 501, json: { error: { code: 'unmocked', message: `unmocked ${method} ${path}` } } })
        if (path === '/admin/status') return route.fulfill({ json: { providers: [], sources: [], recentErrors: [] } })
        if (path === '/admin/job-runs') return route.fulfill({ json: { sources: [] } })
        if (path === '/admin/users') return route.fulfill({ json: { users: [] } })
        if (path === '/admin/match-suggestions') return route.fulfill({ json: { items: [] } })
        if (path === '/admin/library-connections') return route.fulfill({ json: { connections: [] } })
        if (path === '/admin/secrets') return route.fulfill({ json: { enabled: true, secrets: [] } })
        return route.fulfill({ json: { configured: false, enabled: false } })
    })
    await page.route('**/api/v1/admin/settings/authentication**', route => {
        const req = route.request(), path = new URL(req.url()).pathname.replace('/api/v1/admin/settings/authentication', '')
        const fail = (status: number, code: string, message: string) => route.fulfill({ status, json: { error: { code, message, requestId: 'req-1' } } })
        if (path === '/local-administrator' && req.method() === 'GET') return route.fulfill({ json: world.local })
        if (path === '/local-administrator' && req.method() === 'POST') { world.local = { exists: true, disabled: false, canDisable: false }; return route.fulfill({ status: 201, json: { username: 'admin', password: GENERATED, mustChangePassword: true } }) }
        if (path === '/local-administrator/disabled') {
            const disabled = (req.postDataJSON() as { disabled: boolean }).disabled
            if (disabled && !world.local.canDisable) return fail(409, 'last_admin', 'Disabling the local administrator needs another enabled administrator.')
            world.local = { ...world.local, disabled }
            return route.fulfill({ json: world.local })
        }
        if (path === '' && req.method() === 'GET') return route.fulfill({ json: world.auth })
        return route.fallback()
    })
    await page.goto('/settings?tab=authentication')
    await expect(page.getByRole('heading', { name: 'OpenID Connect', exact: true })).toBeVisible()
    return { calls, world }
}
const axe = async (page: Page) => expect((await new AxeBuilder({ page }).analyze()).violations).toEqual([])
const noOverflow = async (page: Page) => expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
const oidc = (page: Page) => page.getByRole('listitem').filter({ has: page.getByRole('heading', { name: 'OpenID Connect', exact: true }) })
const local = (page: Page) => page.getByRole('listitem').filter({ has: page.getByRole('heading', { name: 'Local administrator', exact: true }) })

test('Authentication is a Settings section with the two cards, not a separate page', async ({ page }) => {
    await setup(page)
    await expect(page.getByRole('heading', { name: 'Settings', level: 1 })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Authentication', exact: true })).toHaveAttribute('aria-pressed', 'true')
    await expect(page.getByRole('heading', { name: 'Local administrator', exact: true })).toBeVisible()
    await expect(oidc(page).getByText('Not configured', { exact: false }).first()).toBeVisible()
    await expect(oidc(page).getByLabel('Client secret', { exact: true })).toHaveAttribute('type', 'password')
})

const states: { name: string; patch: Partial<State>; message: string | RegExp }[] = [
    { name: 'not configured', patch: {}, message: 'OIDC is not configured.' },
    { name: 'environment only', patch: { state: 'environment_only', effective: { ...saved, enabled: true }, readOnly: true, secretConfigured: true, working: true }, message: 'Set by environment' },
    { name: 'environment overriding saved', patch: { state: 'environment_overriding_saved', effective: { ...saved, enabled: true }, saved, readOnly: true, secretConfigured: true, working: true, ignored: ['REQUESTARR_OIDC_ISSUER', 'REQUESTARR_OIDC_CLIENT_ID', 'REQUESTARR_OIDC_CLIENT_SECRET'], differing: ['REQUESTARR_OIDC_ISSUER'] }, message: 'Saved settings are ignored' },
    { name: 'environment incomplete', patch: { state: 'environment_incomplete', readOnly: true, missing: ['REQUESTARR_OIDC_CLIENT_SECRET'], effective: { ...saved, enabled: true } }, message: 'Environment configuration is incomplete' },
    { name: 'no master key', patch: { canSaveSecret: false }, message: 'REQUESTARR_SECRETS_KEY is not configured' },
    { name: 'unreadable secret', patch: { ...savedState(), secretUnreadable: true }, message: 'The saved client secret cannot be read' },
    { name: 'saved and disabled', patch: savedState(), message: 'OIDC is off. Saved settings are kept.' },
    { name: 'saved and working', patch: { ...savedState({ enabled: true }), working: true }, message: 'OIDC is on and a provider is active.' },
    { name: 'startup problem', patch: { ...savedState({ enabled: true }), problem: 'OIDC discovery failed: issuer could not be reached' }, message: 'OIDC discovery failed' },
]
for (const width of [1440, 768, 390]) for (const sample of states) {
    test(`${sample.name} at ${width}px is explained and accessible`, async ({ page }) => {
        await page.setViewportSize({ width, height: 1000 })
        await setup(page, sample.patch)
        const card = oidc(page)
        await expect(card.getByText(sample.message, { exact: false }).first()).toBeVisible()
        if (sample.patch.readOnly) {
            await expect(card.getByLabel('Issuer', { exact: true })).toHaveAttribute('readonly', '')
            await expect(card.getByRole('button', { name: 'Save OIDC settings' })).toHaveCount(0)
            await expect(card.getByRole('switch', { name: 'Enable OIDC' })).toBeDisabled()
        }
        if (sample.patch.state === 'environment_only') await expect(card.getByText('Set by environment', { exact: false }).first()).toBeVisible()
        if (sample.patch.missing?.length) await expect(card.getByText('REQUESTARR_OIDC_CLIENT_SECRET', { exact: true })).toBeVisible()
        if (sample.patch.ignored?.length) {
            await expect(card.getByRole('button', { name: 'Discard saved settings' })).toBeVisible()
            await expect(card.getByText(/Differs from the environment: REQUESTARR_OIDC_ISSUER/)).toBeVisible()
        } else await expect(card.getByRole('button', { name: 'Discard saved settings' })).toHaveCount(0)
        if (sample.patch.canSaveSecret === false) await expect(card.getByLabel('Client secret', { exact: true })).toBeDisabled()
        await noOverflow(page)
        await axe(page)
    })
}

test('the redirect address is copyable from the keyboard and announced', async ({ page, context }) => {
    await context.grantPermissions(['clipboard-read', 'clipboard-write'])
    await setup(page)
    await expect(oidc(page).getByLabel('Redirect address', { exact: true })).toHaveValue('https://requestarr.example.test/api/v1/auth/callback')
    await oidc(page).getByRole('button', { name: 'Copy redirect address' }).focus()
    await page.keyboard.press('Enter')
    await expect(page.getByRole('status').filter({ hasText: 'Redirect address copied' })).toBeVisible()
    expect(await page.evaluate(() => navigator.clipboard.readText())).toBe('https://requestarr.example.test/api/v1/auth/callback')
})

for (const width of [1440, 390]) test(`Test names what failed and changes nothing; Save keeps the previous configuration on failure (${width}px)`, async ({ page }) => {
    await page.setViewportSize({ width, height: 1000 })
    const { calls } = await setup(page, { ...savedState({ enabled: true }), working: true })
    let failTest = true
    await page.route('**/api/v1/admin/settings/authentication/test', route => failTest
        ? route.fulfill({ status: 422, json: { error: { code: 'oidc_discovery_failed', message: 'OIDC discovery failed: issuer could not be reached', requestId: 'req-2' } } })
        : route.fulfill({ json: { ok: true } }))
    const card = oidc(page)
    await card.getByLabel('Issuer', { exact: true }).fill('https://changed.example.test')
    await card.getByLabel('Client secret', { exact: true }).fill(SECRET)
    await card.getByRole('button', { name: 'Test OIDC connection' }).focus()
    await page.keyboard.press('Enter')
    await expect(card.getByRole('alert').filter({ hasText: 'issuer could not be reached' })).toBeVisible()
    await expect(card.getByRole('alert')).toContainText('previous configuration')
    await expect(card.getByText('OIDC is on and a provider is active.')).toBeVisible()
    expect(calls.filter(c => c.method === 'PUT')).toHaveLength(0)
    const testBody = calls.find(c => c.path.endsWith('/test'))!.body!
    expect(testBody).toMatchObject({ issuer: 'https://changed.example.test', clientId: 'requestarr', providerName: 'Keycloak', clientSecret: SECRET })
    // A tested secret is not kept in the page.
    await expect(card.getByLabel('Client secret', { exact: true })).toHaveValue('')
    await axe(page)
    failTest = false
    await card.getByRole('button', { name: 'Test OIDC connection' }).press('Enter')
    await expect(card.getByRole('status').filter({ hasText: 'Discovery passed' })).toBeVisible()

    await page.route('**/api/v1/admin/settings/authentication', route => route.request().method() === 'PUT'
        ? route.fulfill({ status: 422, json: { error: { code: 'oidc_discovery_failed', message: 'OIDC discovery failed: provider metadata is invalid', requestId: 'req-3' } } })
        : route.fallback())
    await card.getByLabel('Client secret', { exact: true }).fill(SECRET)
    await card.getByRole('button', { name: 'Save OIDC settings' }).press('Enter')
    await expect(card.getByRole('alert').filter({ hasText: 'metadata is invalid' })).toBeVisible()
    await expect(card.getByLabel('Client secret', { exact: true })).toHaveValue('')
    await expect(card.getByLabel('Issuer', { exact: true })).toHaveValue('https://changed.example.test')
    await expect(card.getByText('OIDC is on and a provider is active.')).toBeVisible()
    const put = calls.filter(c => c.method === 'PUT')
    expect(put).toHaveLength(1)
    expect(put[0].csrf).toBe('test-csrf')
    expect(put[0].body).toEqual({ enabled: true, providerName: 'Keycloak', issuer: 'https://changed.example.test', clientId: 'requestarr', scopes: 'openid profile email', ...groupDefaults, clientSecret: SECRET })
})

test('saving, enabling and disabling work from the keyboard and the secret never reaches browser storage', async ({ page }) => {
    const { calls, world } = await setup(page)
    const card = oidc(page)
    await expect(card.getByRole('switch', { name: 'Enable OIDC' })).toBeEnabled()
    await expect(card.getByRole('button', { name: 'Save OIDC settings' })).toBeDisabled()
    await card.getByLabel('Provider name', { exact: true }).fill('Keycloak')
    await card.getByLabel('Issuer', { exact: true }).fill(saved.issuer)
    await card.getByLabel('Client ID', { exact: true }).fill('requestarr')
    await card.getByLabel('Client secret', { exact: true }).fill(SECRET)
    await page.route('**/api/v1/admin/settings/authentication', route => {
        if (route.request().method() !== 'PUT') return route.fallback()
        world.auth = { ...base, ...savedState() } as State
        return route.fulfill({ json: world.auth })
    })
    await card.getByRole('button', { name: 'Save OIDC settings' }).press('Enter')
    await expect(card.getByRole('status').filter({ hasText: 'OIDC settings saved' })).toBeVisible()
    await expect(card.getByLabel('Client secret', { exact: true })).toHaveValue('')
    await expect(card.getByText('Stored encrypted, never shown again. A client secret is saved; leave this blank to keep it.')).toBeVisible()
    expect(calls.find(c => c.method === 'PUT')!.body).toMatchObject({ enabled: false, clientSecret: SECRET })
    const stores = await page.evaluate(() => JSON.stringify([{ ...localStorage }, { ...sessionStorage }, document.cookie, location.href]))
    expect(stores).not.toContain(SECRET)
    await page.route('**/api/v1/admin/settings/authentication/enabled', route => {
        const enabled = (route.request().postDataJSON() as { enabled: boolean }).enabled
        world.auth = { ...base, ...savedState({ enabled }), working: enabled } as State
        return route.fulfill({ json: world.auth })
    })
    const toggle = card.getByRole('switch', { name: 'Enable OIDC' })
    await toggle.focus()
    await page.keyboard.press('Space')
    await expect(card.getByText('OIDC is on and a provider is active.')).toBeVisible()
    await expect(toggle).toBeChecked()
    await page.keyboard.press('Space')
    await expect(card.getByText('OIDC is off. Saved settings are kept.')).toBeVisible()
    expect(calls.filter(c => c.path.endsWith('/enabled')).map(c => c.body)).toEqual([{ enabled: true }, { enabled: false }])
})

test('an enable that fails discovery says why and leaves the switch where it was', async ({ page }) => {
    await setup(page, savedState())
    await page.route('**/api/v1/admin/settings/authentication/enabled', route => route.fulfill({ status: 422, json: { error: { code: 'oidc_discovery_failed', message: 'OIDC discovery failed: issuer could not be reached', requestId: 'req-4' } } }))
    const card = oidc(page)
    await card.getByRole('switch', { name: 'Enable OIDC' }).press('Space')
    await expect(card.getByRole('alert').filter({ hasText: 'issuer could not be reached' })).toBeVisible()
    await expect(card.getByRole('switch', { name: 'Enable OIDC' })).not.toBeChecked()
})

test('discarding saved settings that the environment overrides', async ({ page }) => {
    const { calls, world } = await setup(page, { state: 'environment_overriding_saved', effective: { ...saved, enabled: true }, saved, readOnly: true, secretConfigured: true, working: true, ignored: ['REQUESTARR_OIDC_ISSUER', 'REQUESTARR_OIDC_CLIENT_ID', 'REQUESTARR_OIDC_CLIENT_SECRET'], differing: [] })
    const card = oidc(page)
    await expect(card.getByText('same values', { exact: false })).toBeVisible()
    await page.route('**/api/v1/admin/settings/authentication', route => {
        if (route.request().method() !== 'DELETE') return route.fallback()
        world.auth = { ...base, state: 'environment_only', effective: { ...saved, enabled: true }, readOnly: true, secretConfigured: true, working: true }
        return route.fulfill({ json: world.auth })
    })
    await card.getByRole('button', { name: 'Discard saved settings' }).press('Enter')
    await expect(card.getByRole('status').filter({ hasText: 'Saved settings discarded' })).toBeVisible()
    await expect(card.getByRole('button', { name: 'Discard saved settings' })).toHaveCount(0)
    expect(calls.filter(c => c.method === 'DELETE').map(c => c.csrf)).toEqual(['test-csrf'])
})

for (const width of [1440, 390]) test(`the local administrator card shows its status and refuses to disable the last administrator (${width}px)`, async ({ page }) => {
    await page.setViewportSize({ width, height: 1000 })
    await setup(page, {}, { exists: true, disabled: false, canDisable: false })
    const card = local(page)
    await expect(card.getByText('Enabled', { exact: true })).toBeVisible()
    await expect(card.getByText('Disabling needs another enabled administrator', { exact: false })).toBeVisible()
    await card.getByRole('button', { name: 'Disable local administrator' }).press('Enter')
    await expect(card.getByRole('alert').filter({ hasText: 'needs another enabled administrator' })).toBeVisible()
    await expect(card.getByText('Enabled', { exact: true })).toBeVisible()
    await axe(page)
})

test('another admin without working OIDC does not allow disabling, and the card says what to do first', async ({ page }) => {
    await setup(page, {}, { exists: true, disabled: false, canDisable: false, otherAdminExists: true, oidcWorking: false })
    const card = local(page)
    await expect(card.getByText('Turn on OIDC and make sure another admin can sign in first', { exact: false })).toBeVisible()
    await expect(card.getByText('give someone the Admin role', { exact: false })).toHaveCount(0)
    await axe(page)
})

test('with nobody else and working OIDC the card asks for another admin', async ({ page }) => {
    await setup(page, {}, { exists: true, disabled: false, canDisable: false, otherAdminExists: false, oidcWorking: true })
    await expect(local(page).getByText('give someone the Admin role', { exact: false })).toBeVisible()
})

test('a refused disable (no working sign-in) is explained in the card', async ({ page }) => {
    await setup(page, {}, { exists: true, disabled: false, canDisable: true })
    await page.route('**/api/v1/admin/settings/authentication/local-administrator/disabled', route => route.fulfill({ status: 409, json: { error: { code: 'no_working_sign_in', message: 'turn on OIDC and make sure another admin can sign in first', requestId: 'r' } } }))
    const card = local(page)
    await card.getByRole('button', { name: 'Disable local administrator' }).press('Enter')
    await expect(card.getByRole('alert').filter({ hasText: 'make sure another admin can sign in first' })).toBeVisible()
})

test('a local administrator can be disabled and enabled again when another administrator exists', async ({ page }) => {
    const { calls } = await setup(page, {}, { exists: true, disabled: false, canDisable: true })
    const card = local(page)
    await card.getByRole('button', { name: 'Disable local administrator' }).press('Enter')
    await expect(card.getByText('Disabled', { exact: true })).toBeVisible()
    await expect(card.getByRole('status').filter({ hasText: 'Local administrator disabled' })).toBeVisible()
    await card.getByRole('button', { name: 'Enable local administrator' }).press('Enter')
    await expect(card.getByText('Enabled', { exact: true })).toBeVisible()
    expect(calls.filter(c => c.path.endsWith('/disabled')).map(c => [c.body, c.csrf])).toEqual([[{ disabled: true }, 'test-csrf'], [{ disabled: false }, 'test-csrf']])
})

for (const width of [1440, 390]) test(`creating the local administrator shows the generated password once (${width}px)`, async ({ page }) => {
    await page.setViewportSize({ width, height: 1000 })
    const { calls } = await setup(page, {}, { exists: false, disabled: false, canDisable: false })
    const card = local(page)
    await expect(card.getByText('No local administrator', { exact: false }).first()).toBeVisible()
    await expect(card.getByLabel('Current password')).toHaveCount(0)
    await card.getByRole('button', { name: 'Create local administrator' }).press('Enter')
    const reveal = card.getByRole('group', { name: 'One-time password' })
    await expect(reveal).toContainText(GENERATED)
    await expect(reveal).toContainText('shown once')
    await expect(reveal).toBeFocused()
    expect(calls.find(c => c.method === 'POST')!.csrf).toBe('test-csrf')
    await axe(page)
    expect(JSON.stringify([{ ...await page.evaluate(() => ({ ...localStorage })) }, await page.evaluate(() => ({ ...sessionStorage }))])).not.toContain(GENERATED)
    await reveal.getByRole('button', { name: 'I have saved the password' }).press('Enter')
    await expect(page.getByText(GENERATED)).toHaveCount(0)
    await expect(card.getByText('Enabled', { exact: true })).toBeVisible()
    await page.reload()
    await expect(page.getByText(GENERATED)).toHaveCount(0)
})

test('creating the local administrator when one exists reports the conflict', async ({ page }) => {
    await setup(page, {}, { exists: false, disabled: false, canDisable: false })
    await page.route('**/api/v1/admin/settings/authentication/local-administrator', route => route.request().method() === 'POST'
        ? route.fulfill({ status: 409, json: { error: { code: 'local_admin_exists', message: 'a local administrator already exists', requestId: 'req-5' } } }) : route.fallback())
    const card = local(page)
    await card.getByRole('button', { name: 'Create local administrator' }).press('Enter')
    await expect(card.getByRole('alert')).toContainText('already exists')
    await expect(page.getByText(GENERATED)).toHaveCount(0)
})

test.describe('change password', () => {
    test('is offered only to the session of the local administrator', async ({ page }) => {
        // An administrator who signed in through OIDC has no local password to change.
        await setup(page, {}, { exists: true, disabled: false, canDisable: true }, false)
        const card = local(page)
        await expect(card.getByText('Enabled', { exact: true })).toBeVisible()
        await expect(card.getByRole('heading', { name: 'Change password' })).toHaveCount(0)
        await expect(card.getByLabel('Current password', { exact: true })).toHaveCount(0)
        await axe(page)
    })
    test('is offered to the local administrator session', async ({ page }) => {
        await setup(page)
        await expect(local(page).getByRole('heading', { name: 'Change password' })).toBeVisible()
    })

    const open = async (page: Page) => {
        const ctx = await setup(page)
        const card = local(page)
        return { ...ctx, card }
    }
    test('validates length and confirmation before asking the server', async ({ page }) => {
        const { calls, card } = await open(page)
        await card.getByLabel('Current password', { exact: true }).fill('old-synthetic-password')
        await card.getByLabel('New password', { exact: true }).fill('short')
        await card.getByLabel('Confirm new password', { exact: true }).fill('short')
        await card.getByRole('button', { name: 'Change password' }).last().press('Enter')
        await expect(card.getByRole('alert')).toContainText('12 to 128 characters')
        await card.getByLabel('New password', { exact: true }).fill('a-long-enough-passphrase')
        await card.getByRole('button', { name: 'Change password' }).last().press('Enter')
        await expect(card.getByRole('alert')).toContainText('do not match')
        expect(calls.filter(c => c.path === '/auth/local/password')).toHaveLength(0)
    })
    test('changes the password, clears the fields and announces it', async ({ page }) => {
        const { calls, card } = await open(page)
        await page.route('**/api/v1/auth/local/password', route => route.fulfill({ status: 204 }))
        await card.getByLabel('Current password', { exact: true }).fill('old-synthetic-password')
        await card.getByLabel('New password', { exact: true }).fill('a-long-enough-passphrase')
        await card.getByLabel('Confirm new password', { exact: true }).fill('a-long-enough-passphrase')
        await card.getByRole('button', { name: 'Change password' }).last().press('Enter')
        await expect(card.getByRole('status').filter({ hasText: 'Password changed' })).toBeVisible()
        await expect(card.getByLabel('Current password', { exact: true })).toHaveValue('')
        await expect(card.getByLabel('New password', { exact: true })).toHaveValue('')
        expect(calls.find(c => c.path === '/auth/local/password')).toMatchObject({ method: 'POST', csrf: 'test-csrf', body: { currentPassword: 'old-synthetic-password', newPassword: 'a-long-enough-passphrase' } })
    })
    for (const [name, status, code, message, shown] of [
        ['a wrong current password', 403, 'current_password_incorrect', 'the current password is incorrect', 'current password is not correct'],
        ['an account without a local password', 403, 'forbidden', 'forbidden', 'Only the local administrator'],
        ['too many attempts', 429, 'too_many_attempts', 'too many attempts', 'Try again in'],
    ] as const) test(`explains ${name}`, async ({ page }) => {
        const { card } = await open(page)
        await page.route('**/api/v1/auth/local/password', route => route.fulfill({ status, headers: { 'Retry-After': '30' }, json: { error: { code, message, requestId: 'req-6' } } }))
        await card.getByLabel('Current password', { exact: true }).fill('old-synthetic-password')
        await card.getByLabel('New password', { exact: true }).fill('a-long-enough-passphrase')
        await card.getByLabel('Confirm new password', { exact: true }).fill('a-long-enough-passphrase')
        await card.getByRole('button', { name: 'Change password' }).last().press('Enter')
        await expect(card.getByRole('alert')).toContainText(shown)
        await expect(card.getByLabel('New password', { exact: true })).toHaveValue('')
    })
    test('is accessible at phone width', async ({ page }) => {
        await page.setViewportSize({ width: 390, height: 1000 })
        await open(page)
        await noOverflow(page)
        await axe(page)
    })
})

test('on a first setup the switch can be ticked first and Save turns OIDC on in one step', async ({ page }) => {
    const { calls, world } = await setup(page)
    const card = oidc(page)
    const toggle = card.getByRole('switch', { name: 'Enable OIDC' })
    await toggle.check()
    await expect(toggle).toBeChecked()
    expect(calls.filter(c => c.path.endsWith('/enabled'))).toHaveLength(0)
    await card.getByLabel('Provider name', { exact: true }).fill('Keycloak')
    await card.getByLabel('Issuer', { exact: true }).fill(saved.issuer)
    await card.getByLabel('Client ID', { exact: true }).fill('requestarr')
    await card.getByLabel('Client secret', { exact: true }).fill(SECRET)
    await expect(card.getByText('nothing is saved and OIDC stays off', { exact: false })).toBeVisible()
    await page.route('**/api/v1/admin/settings/authentication', route => {
        if (route.request().method() !== 'PUT') return route.fallback()
        world.auth = { ...base, ...savedState({ enabled: true }), working: true } as State
        return route.fulfill({ json: world.auth })
    })
    await card.getByRole('button', { name: 'Save and turn on OIDC' }).click()
    await expect(card.getByRole('status').filter({ hasText: 'OIDC settings saved and OIDC turned on' })).toBeVisible()
    await expect(toggle).toBeChecked()
    expect(calls.find(c => c.method === 'PUT')!.body).toMatchObject({ enabled: true, clientSecret: SECRET })
})

test('a first setup that fails the provider check saves nothing and says so', async ({ page }) => {
    await setup(page)
    await page.route('**/api/v1/admin/settings/authentication', route => {
        if (route.request().method() !== 'PUT') return route.fallback()
        return route.fulfill({ status: 422, json: { error: { code: 'oidc_discovery_failed', message: 'OIDC discovery failed: issuer could not be reached', requestId: 'req-9' } } })
    })
    const card = oidc(page)
    await card.getByRole('switch', { name: 'Enable OIDC' }).check()
    await card.getByLabel('Provider name', { exact: true }).fill('Keycloak')
    await card.getByLabel('Issuer', { exact: true }).fill(saved.issuer)
    await card.getByLabel('Client ID', { exact: true }).fill('requestarr')
    await card.getByLabel('Client secret', { exact: true }).fill(SECRET)
    await card.getByRole('button', { name: 'Save and turn on OIDC' }).click()
    await expect(card.getByRole('alert').filter({ hasText: 'issuer could not be reached' })).toContainText('Nothing was saved.')
    await expect(card.getByText('OIDC is not configured', { exact: false }).first()).toBeVisible()
})

// Scopes are a setting (four-roles ticket 04): the default asks for groups, an
// existing setup keeps what it asked for, and Test warns without failing.
for (const width of [1440, 390]) test(`a new setup starts with the groups scope and Save sends the edited scopes (${width}px)`, async ({ page }) => {
    await page.setViewportSize({ width, height: 1000 })
    const { calls, world } = await setup(page)
    const card = oidc(page)
    const scopes = card.getByLabel('Scopes', { exact: true })
    await expect(scopes).toHaveValue('openid profile email groups')
    await expect(card.getByText('openid is always requested', { exact: false })).toBeVisible()
    await noOverflow(page)
    await axe(page)
    await card.getByLabel('Provider name', { exact: true }).fill('Keycloak')
    await card.getByLabel('Issuer', { exact: true }).fill(saved.issuer)
    await card.getByLabel('Client ID', { exact: true }).fill('requestarr')
    await card.getByLabel('Client secret', { exact: true }).fill(SECRET)
    await scopes.fill('profile groups offline_access')
    await page.route('**/api/v1/admin/settings/authentication', route => {
        if (route.request().method() !== 'PUT') return route.fallback()
        world.auth = { ...base, ...savedState({ scopes: 'openid profile groups offline_access' }) } as State
        return route.fulfill({ json: world.auth })
    })
    await card.getByRole('button', { name: 'Save OIDC settings' }).press('Enter')
    await expect(card.getByRole('status').filter({ hasText: 'OIDC settings saved' })).toBeVisible()
    expect(calls.find(c => c.method === 'PUT')!.body).toMatchObject({ scopes: 'profile groups offline_access' })
    // The server's normalised list (openid added first) is what the field shows now.
    await expect(scopes).toHaveValue('openid profile groups offline_access')
    await axe(page)
})

test('a setup saved before scopes existed shows what it already asks for and changing only the scopes can be saved', async ({ page }) => {
    const { calls, world } = await setup(page, savedState())
    const card = oidc(page)
    const scopes = card.getByLabel('Scopes', { exact: true })
    await expect(scopes).toHaveValue('openid profile email')
    await expect(card.getByRole('button', { name: 'Save OIDC settings' })).toBeDisabled()
    await scopes.fill('openid profile email groups')
    await page.route('**/api/v1/admin/settings/authentication', route => {
        if (route.request().method() !== 'PUT') return route.fallback()
        world.auth = { ...base, ...savedState({ scopes: 'openid profile email groups' }) } as State
        return route.fulfill({ json: world.auth })
    })
    await card.getByRole('button', { name: 'Save OIDC settings' }).press('Enter')
    await expect(card.getByRole('status').filter({ hasText: 'OIDC settings saved' })).toBeVisible()
    expect(calls.find(c => c.method === 'PUT')!.body).toEqual({ enabled: false, providerName: 'Keycloak', issuer: saved.issuer, clientId: 'requestarr', scopes: 'openid profile email groups', ...groupDefaults })
})

for (const width of [1440, 390]) test(`Test shows a warning, not a failure, when the provider does not list groups (${width}px)`, async ({ page }) => {
    await page.setViewportSize({ width, height: 1000 })
    const { calls } = await setup(page, savedState({ scopes: 'openid profile email groups' }))
    await page.route('**/api/v1/admin/settings/authentication/test', route => route.fulfill({ json: { ok: true, warnings: ['The provider does not list the groups scope, so the groups claim may be missing from ID tokens.'] } }))
    const card = oidc(page)
    await card.getByLabel('Client secret', { exact: true }).fill(SECRET)
    await card.getByRole('button', { name: 'Test OIDC connection' }).press('Enter')
    await expect(card.getByRole('status').filter({ hasText: 'Discovery passed' })).toBeVisible()
    await expect(card.getByText('does not list the groups scope', { exact: false })).toBeVisible()
    await expect(card.getByRole('alert')).toHaveCount(0)
    expect(calls.find(c => c.path.endsWith('/test'))!.body).toMatchObject({ scopes: 'openid profile email groups' })
    await noOverflow(page)
    await axe(page)
})

test('a passing Test with no warnings shows none', async ({ page }) => {
    await setup(page, savedState())
    await page.route('**/api/v1/admin/settings/authentication/test', route => route.fulfill({ json: { ok: true, warnings: [] } }))
    const card = oidc(page)
    await card.getByRole('button', { name: 'Test OIDC connection' }).press('Enter')
    await expect(card.getByRole('status').filter({ hasText: 'Discovery passed' })).toBeVisible()
    await expect(card.getByText('does not list', { exact: false })).toHaveCount(0)
})

for (const width of [1440, 390]) test(`the scopes field is read-only with the usual message when the environment configures OIDC (${width}px)`, async ({ page }) => {
    await page.setViewportSize({ width, height: 1000 })
    await setup(page, { state: 'environment_only', effective: { ...saved, enabled: true }, readOnly: true, secretConfigured: true, working: true })
    const card = oidc(page)
    await expect(card.getByLabel('Scopes', { exact: true })).toHaveAttribute('readonly', '')
    await expect(card.getByLabel('Scopes', { exact: true })).toHaveValue('openid profile email')
    await expect(card.getByText('Set by environment', { exact: false }).first()).toBeVisible()
    await noOverflow(page)
    await axe(page)
})

// Assign roles from groups (four-roles ticket 05): off by default, the names are
// filled in, saving sends them, and the environment makes them read-only.
const groupsBox = (card: ReturnType<typeof oidc>) => card.getByRole('group', { name: 'Assign roles from groups' })

for (const width of [1440, 390]) test(`Assign roles from groups is off, shows the default names, and warns that it can change roles (${width}px)`, async ({ page }) => {
    await page.setViewportSize({ width, height: 1000 })
    await setup(page, savedState())
    const box = groupsBox(oidc(page))
    await expect(box.getByRole('checkbox', { name: 'Assign roles from groups' })).not.toBeChecked()
    await expect(box.getByText('can change roles', { exact: false })).toBeVisible()
    await expect(box.getByLabel('Group claim')).toHaveValue('groups')
    await expect(box.getByLabel('Superuser group')).toHaveValue('owner')
    await expect(box.getByLabel('Admin group')).toHaveValue('admin')
    await expect(box.getByLabel('Requester group')).toHaveValue('user')
    await expect(box.getByLabel('Viewer group')).toHaveValue('viewer')
    await noOverflow(page)
    await axe(page)
})

for (const width of [1440, 390]) test(`switching mapping on and renaming a group is sent on Save and shown from the server answer (${width}px)`, async ({ page }) => {
    await page.setViewportSize({ width, height: 1000 })
    const { calls, world } = await setup(page, savedState())
    const card = oidc(page)
    const box = groupsBox(card)
    await expect(card.getByRole('button', { name: 'Save OIDC settings' })).toBeDisabled()
    await box.getByRole('checkbox', { name: 'Assign roles from groups' }).check()
    await box.getByLabel('Superuser group').fill('Household-Owners')
    await box.getByLabel('Group claim').fill('roles')
    await page.route('**/api/v1/admin/settings/authentication', route => {
        if (route.request().method() !== 'PUT') return route.fallback()
        world.auth = { ...base, ...savedState({ assignRolesFromGroups: true, ownerGroup: 'Household-Owners', groupsClaim: 'roles' }) } as State
        return route.fulfill({ json: world.auth })
    })
    await card.getByRole('button', { name: 'Save OIDC settings' }).press('Enter')
    await expect(card.getByRole('status').filter({ hasText: 'OIDC settings saved' })).toBeVisible()
    expect(calls.find(c => c.method === 'PUT')!.body).toMatchObject({ assignRolesFromGroups: true, ownerGroup: 'Household-Owners', groupsClaim: 'roles', adminGroup: 'admin', userGroup: 'user', viewerGroup: 'viewer' })
    await expect(box.getByRole('checkbox', { name: 'Assign roles from groups' })).toBeChecked()
    await expect(card.getByRole('button', { name: 'Save OIDC settings' })).toBeDisabled()
    await noOverflow(page)
    await axe(page)
})

for (const width of [1440, 390]) test(`the group settings are read-only when the environment configures OIDC (${width}px)`, async ({ page }) => {
    await page.setViewportSize({ width, height: 1000 })
    await setup(page, { state: 'environment_only', effective: { ...saved, enabled: true }, readOnly: true, secretConfigured: true, working: true })
    const box = groupsBox(oidc(page))
    await expect(box.getByRole('checkbox', { name: 'Assign roles from groups' })).toBeDisabled()
    for (const name of ['Group claim', 'Superuser group', 'Admin group', 'Requester group', 'Viewer group']) await expect(box.getByLabel(name)).toHaveAttribute('readonly', '')
    await expect(box.getByText('Set by environment', { exact: false })).toBeVisible()
    await noOverflow(page)
    await axe(page)
})

test('a response from before the group settings existed shows the defaults with mapping off', async ({ page }) => {
    const { effective } = savedState()
    const old = { ...effective } as Record<string, unknown>
    for (const key of Object.keys(groupDefaults)) delete old[key]
    await setup(page, { ...savedState(), effective: old as typeof saved })
    const box = groupsBox(oidc(page))
    await expect(box.getByRole('checkbox', { name: 'Assign roles from groups' })).not.toBeChecked()
    await expect(box.getByLabel('Admin group')).toHaveValue('admin')
})
