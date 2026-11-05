import { expect, test } from '@playwright/test'
import AxeBuilder from '@axe-core/playwright'

test('sign-in does not depend on obsolete first-run status', async ({ page }) => {
    await page.route('**/api/v1/auth/me', route => route.fulfill({ status: 401, json: { error: { code: 'authentication_required', message: 'Sign in' } } }))
    const obsoleteCalls: string[] = []
    await page.route('**/api/v1/setup/**', route => {
        obsoleteCalls.push(route.request().url())
        return route.fulfill({ status: 404, json: { error: { code: 'not_found', message: 'Not found' } } })
    })
    await page.goto('/login')
    await expect(page.getByRole('heading', { name: 'Administrator sign-in' })).toBeVisible()
    expect(obsoleteCalls).toEqual([])
    await expect(page.getByLabel('One-time setup token')).toHaveCount(0)
    expect((await new AxeBuilder({ page }).analyze()).violations).toEqual([])
})
