import AxeBuilder from '@axe-core/playwright'
import { expect, test, type Page } from '@playwright/test'

// The signed-out screen (D040) in every state of the identity-provider configuration.

async function audit(page: Page) {
    const results = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze()
    expect(results.violations.map(v => `${v.id}: ${v.nodes.map(n => n.target.join(' ')).join(' | ')}`)).toEqual([])
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)
    expect(overflow).toBeLessThanOrEqual(0)
}

type Options = { status: number; json?: unknown }

async function signedOut(page: Page, options: Options) {
    await page.route('**/api/v1/auth/me', route => route.fulfill({ status: 401, json: { error: { code: 'authentication_required', message: 'authentication required', requestId: 'r' } } }))
    await page.route('**/api/v1/auth/options', route => route.fulfill({ status: options.status, json: options.json ?? { error: { code: 'x', message: 'x', requestId: 'r' } } }))
}

const states = {
    enabled: { status: 200, json: { oidcEnabled: true, providerName: 'Keycloak' } },
    disabled: { status: 200, json: { oidcEnabled: false, providerName: '' } },
    failing: { status: 500 },
} satisfies Record<string, Options>

for (const size of [{ name: 'desktop', width: 1280, height: 800 }, { name: 'phone', width: 360, height: 800 }]) {
    test.describe(size.name, () => {
        test.use({ viewport: { width: size.width, height: size.height } })

        test('with OIDC enabled the provider is the main action and Username / Password reveals the local form', async ({ page }) => {
            await signedOut(page, states.enabled)
            await page.goto('/')
            await expect(page.getByRole('heading', { name: 'Sign in to Requestarr' })).toBeVisible()
            await expect(page.getByText(/shared request and availability portal/i)).toBeVisible()
            const main = page.getByRole('link', { name: 'Sign in with Keycloak' })
            await expect(main).toHaveAttribute('href', '/api/v1/auth/login')
            await expect(page.getByLabel('Password')).toHaveCount(0)
            await audit(page)
            const toggle = page.getByRole('button', { name: 'Username / Password' })
            await expect(toggle).toHaveAttribute('aria-expanded', 'false')
            await toggle.click()
            await expect(toggle).toHaveAttribute('aria-expanded', 'true')
            await expect(page.getByLabel('Password')).toBeFocused()
            await expect(page.getByRole('button', { name: 'Sign in', exact: true })).toBeVisible()
            await expect(main).toBeVisible()
            await audit(page)
            await toggle.click()
            await expect(page.getByLabel('Password')).toHaveCount(0)
        })

        test('with OIDC disabled or not configured only the local form is offered', async ({ page }) => {
            await signedOut(page, states.disabled)
            await page.goto('/')
            await expect(page.getByRole('heading', { name: 'Sign in to Requestarr' })).toBeVisible()
            await expect(page.getByText(/shared request and availability portal/i)).toBeVisible()
            await expect(page.getByLabel('Password')).toBeVisible()
            await expect(page.getByRole('link', { name: /Sign in with/ })).toHaveCount(0)
            await expect(page.getByRole('button', { name: 'Username / Password' })).toHaveCount(0)
            await audit(page)
        })

        test('when the options request fails the local form still shows and organization sign-in stays reachable', async ({ page }) => {
            await signedOut(page, states.failing)
            await page.goto('/')
            await expect(page.getByLabel('Password')).toBeVisible()
            await expect(page.getByRole('status')).toContainText('Could not check')
            await expect(page.getByRole('link', { name: 'Sign in with your organization account' })).toHaveAttribute('href', '/api/v1/auth/login')
            await expect(page.getByRole('link', { name: 'Sign in with your organization account' })).toHaveClass(/button/)
            await audit(page)
        })
    })
}

test('the local form at /login is served whatever the options say', async ({ page }) => {
    for (const state of [states.enabled, states.disabled, states.failing]) {
        await page.unroute('**/api/v1/auth/options').catch(() => undefined)
        await signedOut(page, state)
        await page.goto('/login')
        await expect(page.getByRole('heading', { name: 'Administrator sign-in' })).toBeVisible()
        await expect(page.getByLabel('Password')).toBeVisible()
    }
})

test('the administrator form keeps single sign-on one click away when it is configured', async ({ page }) => {
    await signedOut(page, states.enabled)
    await page.goto('/login')
    await expect(page.getByRole('link', { name: 'Sign in with Keycloak' })).toHaveAttribute('href', '/api/v1/auth/login')
    await audit(page)
})

test('the local form at /login is served when the options request never answers', async ({ page }) => {
    await page.route('**/api/v1/auth/me', route => route.fulfill({ status: 401, json: { error: { code: 'authentication_required', message: 'authentication required', requestId: 'r' } } }))
    await page.route('**/api/v1/auth/options', () => new Promise(() => undefined))
    await page.goto('/login')
    await expect(page.getByLabel('Password')).toBeVisible()
})

test('a network error on the options request shows the local form', async ({ page }) => {
    await page.route('**/api/v1/auth/me', route => route.fulfill({ status: 401, json: { error: { code: 'authentication_required', message: 'authentication required', requestId: 'r' } } }))
    await page.route('**/api/v1/auth/options', route => route.abort())
    await page.goto('/')
    await expect(page.getByLabel('Password')).toBeVisible()
})

test('a provider with no name is called the organization account', async ({ page }) => {
    await signedOut(page, { status: 200, json: { oidcEnabled: true, providerName: '' } })
    await page.goto('/')
    await expect(page.getByRole('link', { name: 'Sign in with your organization account' })).toBeVisible()
})
