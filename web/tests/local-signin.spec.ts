import AxeBuilder from '@axe-core/playwright'
import { expect, test, type Page } from '@playwright/test'

// The built-in local administrator (D040): the sign-in form at its own address and
// the forced password change that follows the first sign-in.

async function audit(page: Page) {
    const results = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze()
    expect(results.violations.map(v => `${v.id}: ${v.nodes.map(n => n.target.join(' ')).join(' | ')}`)).toEqual([])
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)
    expect(overflow).toBeLessThanOrEqual(0)
}

const admin = { id: 'u1', role: 'admin', email: '', displayName: 'Administrator' }

async function mockSession(page: Page, state: { session: 'none' | 'must-change' | 'ready' }) {
    await page.route('**/api/v1/auth/me', route => state.session === 'none'
        ? route.fulfill({ status: 401, json: { error: { code: 'authentication_required', message: 'authentication required', requestId: 'r' } } })
        : route.fulfill({ json: { user: admin, csrfToken: 'csrf-token', mustChangePassword: state.session === 'must-change' } }))
    await page.route('**/api/v1/notifications/summary', route => route.fulfill({ json: { unread: 0 } }))
    await page.route('**/api/v1/admin/checklist', route => route.fulfill({ json: { dismissed: true, visible: false, items: [] } }))
    await page.route('**/api/v1/issues/summary', route => route.fulfill({ json: { open: 0 } }))
    await page.route('**/api/v1/requests/summary', route => route.fulfill({ json: { requested: 0 } }))
    await page.route('**/api/v1/requests**', route => route.fulfill({ json: { items: [], nextCursor: null } }))
}

for (const size of [{ name: 'desktop', width: 1280, height: 800 }, { name: 'phone', width: 360, height: 800 }]) {
    test.describe(size.name, () => {
        test.use({ viewport: { width: size.width, height: size.height } })

        test('the local sign-in form is served at /login and passes the accessibility audit', async ({ page }) => {
            await mockSession(page, { session: 'none' })
            await page.goto('/login')
            await expect(page.getByRole('heading', { name: 'Administrator sign-in' })).toBeVisible()
            await expect(page.getByLabel('Username')).toHaveValue('admin')
            await expect(page.getByLabel('Password')).toHaveAttribute('type', 'password')
            await audit(page)
        })

        test('the forced password-change screen passes the accessibility audit', async ({ page }) => {
            await mockSession(page, { session: 'must-change' })
            await page.goto('/')
            await expect(page.getByRole('heading', { name: 'Choose a new password' })).toBeVisible()
            await audit(page)
        })
    })
}

test('signing in with the one-time password leads to the forced change, then the app', async ({ page }) => {
    const state = { session: 'none' as 'none' | 'must-change' | 'ready' }
    await mockSession(page, state)
    let loginBody: unknown, loginOrigin: string | undefined
    await page.route('**/api/v1/auth/local/login', route => {
        loginBody = route.request().postDataJSON()
        loginOrigin = route.request().headers()['content-type']
        state.session = 'must-change'
        return route.fulfill({ json: { user: admin, mustChangePassword: true } })
    })
    let changeBody: unknown, changeCsrf: string | undefined
    await page.route('**/api/v1/auth/local/password', route => {
        changeBody = route.request().postDataJSON()
        changeCsrf = route.request().headers()['x-csrf-token']
        state.session = 'ready'
        return route.fulfill({ status: 204 })
    })
    await page.goto('/login')
    await page.getByLabel('Password').fill('one-time-password-from-the-log')
    await page.getByRole('button', { name: 'Sign in' }).click()
    await expect(page.getByRole('heading', { name: 'Choose a new password' })).toBeVisible()
    expect(loginBody).toEqual({ username: 'admin', password: 'one-time-password-from-the-log' })
    expect(loginOrigin).toContain('application/json')
    // Nothing else of the app is offered while the change is pending.
    await expect(page.getByRole('navigation', { name: 'Main navigation' })).toHaveCount(0)

    await page.getByLabel('Current password').fill('one-time-password-from-the-log')
    await page.getByLabel('New password', { exact: true }).fill('my own chosen passphrase')
    await page.getByLabel('Confirm new password').fill('my own chosen passphrase')
    await page.getByRole('button', { name: 'Change password' }).click()
    await expect(page.getByRole('navigation', { name: 'Main navigation' })).toBeVisible()
    expect(changeBody).toEqual({ currentPassword: 'one-time-password-from-the-log', newPassword: 'my own chosen passphrase' })
    expect(changeCsrf).toBe('csrf-token')
})

test('a wrong password shows one uniform message and a delay says how long to wait', async ({ page }) => {
    await mockSession(page, { session: 'none' })
    let calls = 0
    await page.route('**/api/v1/auth/local/login', route => {
        calls++
        return calls === 1
            ? route.fulfill({ status: 401, json: { error: { code: 'invalid_credentials', message: 'Incorrect username or password.', requestId: 'r1' } } })
            : route.fulfill({ status: 429, headers: { 'Retry-After': '8' }, json: { error: { code: 'too_many_attempts', message: 'Too many attempts. Wait a moment and try again.', requestId: 'r2' } } })
    })
    await page.goto('/login')
    await page.getByLabel('Password').fill('not the password')
    await page.getByRole('button', { name: 'Sign in' }).click()
    await expect(page.getByRole('alert')).toHaveText('Incorrect username or password.')
    await expect(page.getByLabel('Password')).toHaveValue('')
    await page.getByLabel('Password').fill('another try')
    await page.getByRole('button', { name: 'Sign in' }).click()
    await expect(page.getByRole('alert')).toContainText('Too many attempts')
    await expect(page.getByRole('alert')).toContainText('8 seconds')
    await audit(page)
})

test('the forced change refuses a short password and a mismatch before asking the server', async ({ page }) => {
    await mockSession(page, { session: 'must-change' })
    let called = false
    await page.route('**/api/v1/auth/local/password', route => { called = true; return route.fulfill({ status: 204 }) })
    await page.goto('/')
    await page.getByLabel('Current password').fill('the one time password')
    await page.getByLabel('New password', { exact: true }).fill('short')
    await page.getByLabel('Confirm new password').fill('short')
    await page.getByRole('button', { name: 'Change password' }).click()
    await expect(page.getByRole('alert')).toContainText('12 to 128 characters')
    await page.getByLabel('New password', { exact: true }).fill('a long enough passphrase')
    await page.getByLabel('Confirm new password').fill('a different long passphrase')
    await page.getByRole('button', { name: 'Change password' }).click()
    await expect(page.getByRole('alert')).toContainText('do not match')
    expect(called).toBe(false)
})

test('the server explains a wrong current password', async ({ page }) => {
    await mockSession(page, { session: 'must-change' })
    await page.route('**/api/v1/auth/local/password', route => route.fulfill({ status: 403, json: { error: { code: 'current_password_incorrect', message: 'The current password is incorrect.', requestId: 'r' } } }))
    await page.goto('/')
    await page.getByLabel('Current password').fill('wrong one here')
    await page.getByLabel('New password', { exact: true }).fill('a long enough passphrase')
    await page.getByLabel('Confirm new password').fill('a long enough passphrase')
    await page.getByRole('button', { name: 'Change password' }).click()
    await expect(page.getByRole('alert')).toContainText('The current password is incorrect.')
})

test('the OIDC button stays on the signed-out screen, with a way to the local form', async ({ page }) => {
    await mockSession(page, { session: 'none' })
    await page.route('**/api/v1/auth/options', route => route.fulfill({ json: { oidcEnabled: true, providerName: 'Keycloak' } }))
    await page.goto('/')
    await expect(page.getByRole('link', { name: 'Sign in with Keycloak' })).toHaveAttribute('href', '/api/v1/auth/login')
    await page.getByRole('link', { name: 'Administrator sign-in' }).click()
    await expect(page).toHaveURL(/\/login$/)
    await expect(page.getByRole('heading', { name: 'Administrator sign-in' })).toBeVisible()
})
