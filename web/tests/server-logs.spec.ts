import { expect, test, type Page } from '@playwright/test'
import type { ServerLogEntry } from '../src/api'
import { capabilitiesFor } from './capabilities'

const entries: ServerLogEntry[] = [
    { seq: 1, time: '2026-11-07T12:00:00Z', level: 'INFO', message: 'requestarr listening', attrs: [{ key: 'addr', value: ':8080' }] },
    { seq: 2, time: '2026-11-07T12:01:00Z', level: 'WARN', message: 'connection check failed', attrs: [{ key: 'target', value: 'romm' }, { key: 'code', value: 'response_size' }] },
]

async function mock(page: Page, role: string) {
    const calls: string[] = []
    await page.route('**/api/v1/**', async route => {
        const url = new URL(route.request().url())
        const path = url.pathname.replace('/api/v1', '')
        calls.push(`${path}${url.search}`)
        if (path === '/auth/me') return route.fulfill({ json: { user: { id: 'u1', role, displayName: 'Owner', email: 'owner@example.test' }, capabilities: capabilitiesFor(role), csrfToken: 'csrf' } })
        if (path === '/admin/status') return route.fulfill({ json: { providers: [], sources: [], recentErrors: [] } })
        if (path === '/admin/logs') {
            const level = url.searchParams.get('level')
            return route.fulfill({ json: { entries: level === 'warn' ? entries.filter(e => e.level !== 'INFO') : entries, capacity: 1000 } })
        }
        if (path === '/admin/job-runs') return route.fulfill({ json: { sources: [] } })
        if (path === '/admin/users') return route.fulfill({ json: { users: [] } })
        if (path === '/admin/match-suggestions') return route.fulfill({ json: { items: [] } })
        if (path === '/notifications') return route.fulfill({ json: { items: [] } })
        return route.fulfill({ json: {} })
    })
    return calls
}

test('a superuser reads the server logs newest first, filters them and narrows the level', async ({ page }) => {
    const calls = await mock(page, 'superuser')
    await page.goto('/settings?tab=logs')
    const table = page.getByRole('region', { name: 'Server log records' })
    await expect(table.getByRole('row')).toHaveCount(3)
    await expect(table.getByRole('row').nth(1)).toContainText('connection check failed')
    await expect(table.getByRole('row').nth(1)).toContainText('code=response_size')

    await page.getByLabel('Filter').fill('listening')
    await expect(table.getByRole('row')).toHaveCount(2)
    await expect(page.getByRole('status').filter({ hasText: 'records shown' })).toHaveText('1 of 2 records shown.')
    await page.getByLabel('Filter').fill('')

    await page.getByLabel('Show').selectOption('warn')
    await expect(table.getByRole('row')).toHaveCount(2)
    expect(calls).toContain('/admin/logs?level=warn')
})

test('an admin has no Logs tab', async ({ page }) => {
    const calls = await mock(page, 'admin')
    await page.goto('/settings')
    await expect(page.getByRole('button', { name: 'Activity', exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Logs', exact: true })).toHaveCount(0)
    expect(calls.some(call => call.startsWith('/admin/logs'))).toBe(false)
})
