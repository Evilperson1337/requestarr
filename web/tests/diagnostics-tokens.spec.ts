import { openNav } from './nav'
import AxeBuilder from '@axe-core/playwright'
import { expect, test, type Page } from '@playwright/test'
import type { DiagnosticsToken } from '../src/api'
import { capabilitiesFor } from './capabilities'

// Settings, Logs: diagnostics tokens (D043). Every response is a deterministic mock and every
// token value is synthetic.
const VALUE = 'rqd_SyntheticTokenValueForTestOnly0123456789abc'
const existing: DiagnosticsToken = { id: '00000000-0000-4000-8000-000000000001', name: 'old laptop', createdAt: '2026-10-01T09:00:00Z', expiresAt: '2026-12-01T09:00:00Z', lastUsedAt: '2026-11-07T10:00:00Z' }

type Call = { path: string; method: string; body: Record<string, unknown> | null; csrf?: string }

type Options = { tokens?: DiagnosticsToken[]; createFails?: { status: number; code: string; message: string }; listFails?: { on: boolean } }

async function open(page: Page, role = 'superuser', options: Options = {}) {
    const calls: Call[] = []
    const world = { tokens: options.tokens ?? [existing] }
    await page.route('**/api/v1/**', async route => {
        const req = route.request(), path = new URL(req.url()).pathname.replace('/api/v1', ''), method = req.method()
        if (path === '/auth/me') return route.fulfill({ json: { user: { id: 'u1', role, displayName: 'Owner', email: 'owner@example.test' }, capabilities: capabilitiesFor(role), csrfToken: 'csrf' } })
        if (path.startsWith('/admin/diagnostics-tokens')) {
            calls.push({ path, method, body: req.postData() ? (req.postDataJSON() as Record<string, unknown>) : null, csrf: req.headers()['x-csrf-token'] })
            if (method === 'GET') {
                if (options.listFails?.on) return route.fulfill({ status: 500, json: { error: { code: 'diagnostics_token_failed', message: 'could not complete the diagnostics token request', requestId: 'r1' } } })
                return route.fulfill({ json: { tokens: world.tokens, maxActive: 5 } })
            }
            if (method === 'POST' && options.createFails) return route.fulfill({ status: options.createFails.status, json: { error: { code: options.createFails.code, message: options.createFails.message, requestId: 'r2' } } })
            if (method === 'POST') {
                const body = req.postDataJSON() as { name: string; lifetimeDays: number }
                const token: DiagnosticsToken = { id: '00000000-0000-4000-8000-000000000002', name: body.name, createdAt: '2026-11-08T12:00:00Z', expiresAt: '2026-12-08T12:00:00Z', lastUsedAt: null }
                world.tokens = [...world.tokens, token]
                return route.fulfill({ status: 201, json: { value: VALUE, token } })
            }
            if (method === 'DELETE') { const id = path.split('/').pop(); world.tokens = world.tokens.filter(t => t.id !== id); return route.fulfill({ status: 204, body: '' }) }
        }
        if (path === '/admin/logs') return route.fulfill({ json: { entries: [], capacity: 1000 } })
        if (path === '/admin/status') return route.fulfill({ json: { providers: [], sources: [], recentErrors: [] } })
        if (path === '/admin/job-runs') return route.fulfill({ json: { sources: [] } })
        if (path === '/admin/users') return route.fulfill({ json: { users: [] } })
        if (path === '/admin/match-suggestions') return route.fulfill({ json: { items: [] } })
        if (path === '/notifications') return route.fulfill({ json: { items: [] } })
        return route.fulfill({ json: {} })
    })
    return calls
}
const axe = async (page: Page) => expect((await new AxeBuilder({ page }).analyze()).violations).toEqual([])
const noOverflow = async (page: Page) => expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)

for (const size of [{ name: 'desktop', width: 1280, height: 900 }, { name: 'phone', width: 390, height: 844 }]) {
    test(`a superuser creates a token, sees it once, lists it and revokes it at ${size.name} width`, async ({ page, context }) => {
        await context.grantPermissions(['clipboard-read', 'clipboard-write'])
        await page.setViewportSize({ width: size.width, height: size.height })
        const calls = await open(page)
        await page.goto('/logs')
        const list = page.getByRole('region', { name: 'Diagnostics token list' })
        await expect(list.getByRole('row')).toHaveCount(2)
        await expect(list.getByRole('row').nth(1)).toContainText('old laptop')
        await axe(page); await noOverflow(page)

        await page.getByLabel('Token name').fill('  my agent  ')
        await page.getByLabel('Lifetime').selectOption('90')
        await page.getByRole('button', { name: 'Create token' }).click()

        const once = page.getByRole('group', { name: /New token: my agent/ })
        await expect(once).toBeVisible()
        await expect(once).toContainText(VALUE)
        await expect(once).toContainText('only time the token is shown')
        await expect(once).toBeFocused()
        await expect(list.getByRole('row')).toHaveCount(3)
        await expect(list.getByRole('row').nth(2)).toContainText('my agent')
        await expect(list.getByRole('row').nth(2)).toContainText('Never')
        expect(calls.find(c => c.method === 'POST')).toEqual({ path: '/admin/diagnostics-tokens', method: 'POST', body: { name: 'my agent', lifetimeDays: 90 }, csrf: 'csrf' })
        await axe(page); await noOverflow(page)

        await once.getByRole('button', { name: 'Copy token' }).click()
        await expect(once.getByRole('status')).toHaveText('Token copied.')
        expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(VALUE)

        await once.getByRole('button', { name: 'I have saved the token' }).click()
        await expect(page.getByRole('group', { name: /New token/ })).toHaveCount(0)
        await expect(page.getByText(VALUE)).toHaveCount(0)
        expect(await page.content()).not.toContain(VALUE)
        // Reloading shows the list again and never the value.
        await page.reload()
        await expect(list.getByRole('row')).toHaveCount(3)
        expect(await page.content()).not.toContain(VALUE)

        await list.getByRole('button', { name: 'Revoke old laptop' }).click()
        await expect(page.getByRole('status').filter({ hasText: 'Revoked old laptop' })).toBeVisible()
        await expect(list.getByRole('row')).toHaveCount(2)
        await expect(list.getByRole('button', { name: 'Revoke old laptop' })).toHaveCount(0)
        expect(calls.find(c => c.method === 'DELETE')).toEqual({ path: `/admin/diagnostics-tokens/${existing.id}`, method: 'DELETE', body: null, csrf: 'csrf' })
        await axe(page); await noOverflow(page)
    })
}

test('revoking the token whose value is on screen clears the value', async ({ page }) => {
    await open(page)
    await page.goto('/logs')
    await page.getByLabel('Token name').fill('temp')
    await page.getByRole('button', { name: 'Create token' }).click()
    await expect(page.getByText(VALUE)).toBeVisible()
    await page.getByRole('button', { name: 'Revoke temp' }).click()
    await expect(page.getByText(VALUE)).toHaveCount(0)
})

test('an admin has no diagnostics tokens card and makes no request for them', async ({ page }) => {
    const calls = await open(page, 'admin')
    await page.goto('/logs')
    await expect(page.getByRole('heading', { name: 'Diagnostics tokens' })).toHaveCount(0)
    expect(calls).toEqual([])
})

const farFuture = (n: number): DiagnosticsToken => ({ id: `00000000-0000-4000-8000-0000000001${String(n).padStart(2, '0')}`, name: `agent ${n}`, createdAt: '2026-10-01T09:00:00Z', expiresAt: '2099-01-01T00:00:00Z', lastUsedAt: null })

for (const size of [{ name: 'desktop', width: 1280, height: 900 }, { name: 'phone', width: 390, height: 844 }]) {
    for (const failure of [
        { status: 409, code: 'diagnostics_token_limit', message: 'there are already five active diagnostics tokens; revoke one first' },
        { status: 422, code: 'invalid_diagnostics_token', message: 'a diagnostics token needs a plain name of 1 to 80 characters' },
    ]) {
        test(`a create answered ${failure.status} shows an alert and no token at ${size.name} width`, async ({ page }) => {
            await page.setViewportSize({ width: size.width, height: size.height })
            await open(page, 'superuser', { createFails: failure })
            await page.goto('/logs')
            await page.getByLabel('Token name').fill('my agent')
            await page.getByRole('button', { name: 'Create token' }).click()
            await expect(page.getByRole('alert').filter({ hasText: failure.message })).toBeVisible()
            await expect(page.getByRole('group', { name: /New token/ })).toHaveCount(0)
            await axe(page); await noOverflow(page)
        })
    }

    test(`Create is disabled at the active limit and says why at ${size.name} width`, async ({ page }) => {
        await page.setViewportSize({ width: size.width, height: size.height })
        await open(page, 'superuser', { tokens: [1, 2, 3, 4, 5].map(farFuture) })
        await page.goto('/logs')
        await page.getByLabel('Token name').fill('one more')
        await expect(page.getByRole('button', { name: 'Create token' })).toBeDisabled()
        await expect(page.getByText('Revoke a token to make another.')).toBeVisible()
        await axe(page); await noOverflow(page)
    })

    test(`a failed list load shows Retry, and Retry recovers at ${size.name} width`, async ({ page }) => {
        await page.setViewportSize({ width: size.width, height: size.height })
        const listFails = { on: true }
        await open(page, 'superuser', { listFails })
        await page.goto('/logs')
        const alert = page.getByRole('alert').filter({ hasText: 'could not complete the diagnostics token request' })
        await expect(alert).toBeVisible()
        await axe(page); await noOverflow(page)
        listFails.on = false
        await alert.getByRole('button', { name: 'Retry' }).click()
        await expect(page.getByRole('region', { name: 'Diagnostics token list' }).getByRole('row')).toHaveCount(2)
        await expect(page.getByRole('button', { name: 'Retry' })).toHaveCount(0)
        await axe(page); await noOverflow(page)
    })

    test(`an expired token shows the Expired badge at ${size.name} width`, async ({ page }) => {
        await page.setViewportSize({ width: size.width, height: size.height })
        await open(page, 'superuser', { tokens: [{ ...existing, expiresAt: '2020-01-01T00:00:00Z' }, farFuture(1)] })
        await page.goto('/logs')
        const rows = page.getByRole('region', { name: 'Diagnostics token list' }).getByRole('row')
        await expect(rows.nth(1)).toContainText('Expired')
        await expect(rows.nth(2)).not.toContainText('Expired')
        await axe(page); await noOverflow(page)
    })

    test(`the new token value disappears when another page is opened and back at ${size.name} width`, async ({ page }) => {
        await page.setViewportSize({ width: size.width, height: size.height })
        await open(page)
        await page.goto('/logs')
        await page.getByLabel('Token name').fill('temp')
        await page.getByRole('button', { name: 'Create token' }).click()
        await expect(page.getByText(VALUE)).toBeVisible()
        await axe(page); await noOverflow(page)
        await openNav(page)
        await page.getByRole('navigation', { name: 'Main navigation' }).getByRole('link', { name: 'Settings' }).click()
        await expect(page.getByText(VALUE)).toHaveCount(0)
        await openNav(page)
        await page.getByRole('navigation', { name: 'Main navigation' }).getByRole('link', { name: 'Logs' }).click()
        await expect(page.getByRole('heading', { name: 'Diagnostics tokens' })).toBeVisible()
        await expect(page.getByText(VALUE)).toHaveCount(0)
        expect(await page.content()).not.toContain(VALUE)
        await axe(page); await noOverflow(page)
    })
}
