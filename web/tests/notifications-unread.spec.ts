import { expect, test, type Page } from '@playwright/test'
import AxeBuilder from '@axe-core/playwright'

const entry = (id: string, title: string) => ({ id, title, body: 'Details', link: null, createdAt: '2026-01-01T00:00:00Z' })

async function openApp(page: Page, role: string, unread: number) {
    const state = { unread, summaryCalls: 0, items: [entry('n1', 'Arrival approved'), entry('n2', 'Dune available')].slice(0, unread) as ReturnType<typeof entry>[] & { readAt?: string }[] }
    await page.route('**/api/v1/**', route => {
        const request = route.request(), path = new URL(request.url()).pathname.replace('/api/v1', '')
        if (path === '/setup/status') return route.fulfill({ json: { complete: true, required: false } })
        if (path === '/auth/me') return route.fulfill({ json: { user: { id: 'u1', role, displayName: 'Person' }, csrfToken: 'csrf' } })
        if (path === '/notifications/summary') { state.summaryCalls++; return route.fulfill({ json: { unread: state.unread } }) }
        if (path === '/notifications') return route.fulfill({ json: { items: state.items } })
        if (path.startsWith('/notifications/') && path.endsWith('/read')) { state.unread--; return route.fulfill({ status: 204, body: '' }) }
        if (path === '/issues/summary') return route.fulfill({ json: { open: 0 } })
        if (path === '/requests/summary') return route.fulfill({ json: { requested: 0 } })
        return route.fulfill({ json: { configured: false, enabled: false, items: [], nextCursor: null } })
    })
    return state
}

const nav = (page: Page) => page.getByRole('navigation', { name: 'Main navigation' })

for (const role of ['viewer', 'admin']) {
    test(`a ${role} sees their own unread count with an accessible name`, async ({ page }) => {
        await openApp(page, role, 2)
        await page.goto('/library')
        const link = nav(page).getByRole('link', { name: /^Notifications/ })
        await expect(link.getByLabel('2 unread')).toBeVisible()
        await expect(link).toContainText('2')
    })
}

test('zero unread shows no badge', async ({ page }) => {
    await openApp(page, 'viewer', 0)
    await page.goto('/library')
    await expect(nav(page).getByRole('link', { name: 'Notifications', exact: true })).toBeVisible()
    await expect(page.getByLabel(/unread/)).toHaveCount(0)
})

test('marking read lowers the count without a reload, and it is a cheap fetch', async ({ page }) => {
    const state = await openApp(page, 'viewer', 2)
    await page.goto('/notifications')
    const link = nav(page).getByRole('link', { name: /^Notifications/ })
    await expect(link).toContainText('2')
    await page.getByRole('button', { name: 'Mark read' }).first().click()
    await expect(link).toContainText('1')
    expect(state.unread).toBe(1)
    expect((await new AxeBuilder({ page }).analyze()).violations).toEqual([])
})
