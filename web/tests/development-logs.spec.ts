import { expect, test, type Page, type Route } from '@playwright/test'
import type { DevelopmentLogEntry } from '../src/api'
import { capabilitiesFor } from './capabilities'

const record: DevelopmentLogEntry = {
    seq: 1, time: '2026-10-27T12:00:00Z', target: 'igdb', stage: 'oauth',
    outcome: 'failure', code: 'http_status', httpStatus: 401, durationMs: 24,
}

async function mock(page: Page, options: { enabled?: boolean; role?: string; statusCode?: number } = {}) {
    const state = {
        calls: [] as string[], entries: [] as DevelopmentLogEntry[], logStatus: 200,
        logs: null as ((route: Route) => Promise<void>) | null,
        status: null as ((route: Route) => Promise<void>) | null,
    }
    await page.route('**/api/v1/**', async route => {
        const request = route.request()
        const path = new URL(request.url()).pathname.replace('/api/v1', '')
        state.calls.push(`${request.method()} ${path}`)
        if (path === '/auth/me') return route.fulfill({ json: { user: { id: 'admin-1', role: options.role ?? 'admin', displayName: 'Test admin', email: 'admin@example.test' }, capabilities: capabilitiesFor(options.role ?? 'admin'), csrfToken: 'csrf' } })
        if (path === '/auth/logout') return route.fulfill({ status: 204 })
        if (path === '/admin/status') {
            if (state.status) return state.status(route)
            return route.fulfill({ status: options.statusCode ?? 200, json: { providers: [{ key: 'tmdb', name: 'TMDB', configured: true }], sources: [{ slug: 'jellyfin', name: 'Jellyfin', configured: true, lastRun: null, lastSuccessAt: null, lastSnapshotAt: null, lastError: '' }], recentErrors: [], ...(options.enabled === undefined ? {} : { developmentLogsEnabled: options.enabled }) } })
        }
        if (path === '/admin/development-logs') {
            if (state.logs) return state.logs(route)
            return route.fulfill({ status: state.logStatus, json: state.logStatus === 200 ? { entries: state.entries, capacity: 200 } : { error: { code: 'private_error', message: 'RAW_ERROR_SECRET_MUST_NOT_RENDER' } } })
        }
        if (path === '/admin/job-runs') return route.fulfill({ json: { sources: [] } })
        if (path === '/admin/users') return route.fulfill({ json: { users: [] } })
        if (path === '/admin/match-suggestions') return route.fulfill({ json: { items: [] } })
        if (path === '/admin/secrets') return route.fulfill({ json: { enabled: false, secrets: [] } })
        if (path === '/admin/library-connections') return route.fulfill({ json: { connections: [{ slug: 'jellyfin', name: 'Jellyfin', url: 'https://jf.example.test', urlFromEnvironment: false, urlEnv: 'REQUESTARR_JELLYFIN_URL', saved: true, takesUsername: false, username: '', active: true, missing: [], credentials: [] }] } })
        if (path === '/notifications') return route.fulfill({ json: { items: [] } })
        return route.fulfill({ json: {} })
    })
    return state
}

// Diagnostics live under Settings > Activity.
async function activity(page: Page) { await page.getByRole('button', { name: 'Activity', exact: true }).click() }
async function tab(page: Page, name: string) { await page.getByRole('button', { name, exact: true }).click() }

const summary = (page: Page) => page.locator('summary').filter({ hasText: 'Development connection logs' })
const logCalls = (calls: string[]) => calls.filter(call => call === 'GET /admin/development-logs')

for (const status of [401, 403]) {
    for (const action of ['sync', 'role', 'suggestions', 'review', 'secret', 'clear secret', 'secret status']) {
        test(`Settings ${action} authorization loss ${status} clears loaded diagnostics`, async ({ page }) => {
            const state = await mock(page, { enabled: true })
            state.entries = [record]
            let fail = false
            let releaseStatus!: () => void
            const statusGate = new Promise<void>(resolve => { releaseStatus = resolve })
            await page.route('**/api/v1/admin/**', async route => {
                const path = new URL(route.request().url()).pathname.replace('/api/v1/admin/', '')
                const denied = () => route.fulfill({ status, json: { error: { code: 'unauthorized', message: 'Authorization lost' } } })
                if (path === 'sources/jellyfin/sync' || path === 'users/other/role') return denied()
                if (path === 'users') return route.fulfill({ json: { users: [{ id: 'other', displayName: 'Other user', role: 'viewer', roleChoices: ['viewer', 'requester'] }] } })
                if (path === 'match-suggestions' && fail) return denied()
                if (path === 'match-suggestions') return route.fulfill({ json: { items: [{ id: 'candidate', catalogTitle: 'Test movie', sourceName: 'Test library', confidence: 'title_year' }] } })
                if (path === 'match-suggestions/candidate/accept') return denied()
                if (path === 'secrets/tmdb_read_token') return denied()
                if (path === 'secrets') {
                    if (action === 'secret status') { await statusGate; return denied() }
                    return route.fulfill({ json: { enabled: true, secrets: [{ name: 'tmdb_read_token', label: 'TMDb read token', source: 'stored', configured: true }] } })
                }
                return route.fallback()
            })
            await page.goto('/settings')
            await activity(page)
            await summary(page).click()
            await expect(page.getByRole('cell', { name: 'igdb', exact: true })).toBeVisible()
            fail = true
            if (action === 'sync') { await tab(page, 'Integrations'); await page.getByRole('button', { name: 'Sync Jellyfin now', exact: true }).click() }
            if (action === 'role') { await tab(page, 'Users'); await page.getByLabel('Role for Other user').selectOption('requester') }
            if (action === 'suggestions') { await tab(page, 'Matches'); await page.getByRole('button', { name: 'Refresh suggestions' }).click() }
            if (action === 'review') { await tab(page, 'Matches'); await page.getByRole('button', { name: 'Accept match for Test movie from Test library' }).click() }
            if (action === 'clear secret' || action === 'secret' || action === 'secret status') { await tab(page, 'Integrations'); await page.getByRole('listitem').filter({ hasText: 'TMDB' }).getByRole('button', { name: /Settings|Set up/ }).click() }
            if (action === 'clear secret') await page.getByRole('button', { name: 'Clear stored value', exact: true }).click()
            if (action === 'secret') {
                await page.getByLabel('TMDb read token', { exact: true }).fill('test-value')
                await page.getByRole('button', { name: 'Save TMDb read token', exact: true }).click()
            }
            releaseStatus()
            await expect(summary(page)).toHaveCount(0)
            await expect(page.getByRole('region', { name: 'Development connection records' })).toHaveCount(0)
            expect(logCalls(state.calls)).toHaveLength(1) // No diagnostic refresh observes the auth failure.
        })
    }

    test(`Settings child authorization loss ${status} cancels a pending log refresh`, async ({ page }) => {
        const state = await mock(page, { enabled: true })
        state.entries = [record]
        await page.goto('/settings')
        await activity(page)
        await summary(page).click()
        await expect(page.getByRole('cell', { name: 'igdb', exact: true })).toBeVisible()
        let release!: () => void
        const gate = new Promise<void>(resolve => { release = resolve })
        let aborted = false
        page.on('requestfailed', request => { if (request.url().endsWith('/admin/development-logs')) aborted = true })
        state.logs = async route => { await gate; await route.fulfill({ json: { entries: [record], capacity: 200 } }).catch(() => undefined) }
        await page.getByRole('button', { name: 'Refresh logs' }).click()
        await expect.poll(() => logCalls(state.calls).length).toBe(2)
        await page.route('**/api/v1/admin/match-suggestions', route => route.fulfill({ status, json: { error: { message: 'Authorization lost' } } }))
        await tab(page, 'Matches')
        await page.getByRole('button', { name: 'Refresh suggestions' }).click()
        await expect(summary(page)).toHaveCount(0)
        await expect.poll(() => aborted).toBe(true)
        release()
        await expect(page.getByRole('region', { name: 'Development connection records' })).toHaveCount(0)
        expect(logCalls(state.calls)).toHaveLength(2)
    })

    test(`Settings authorization loss ${status} cancels a late capability response`, async ({ page }) => {
        const state = await mock(page, { enabled: true })
        let release!: () => void
        const gate = new Promise<void>(resolve => { release = resolve })
        state.status = async route => { await gate; await route.fulfill({ json: { developmentLogsEnabled: true, providers: [], sources: [], recentErrors: [] } }).catch(() => undefined) }
        let aborted = false
        page.on('requestfailed', request => { if (request.url().endsWith('/admin/status')) aborted = true })
        await page.goto('/settings')
        await activity(page)
        await expect.poll(() => state.calls.includes('GET /admin/status')).toBe(true)
        await tab(page, 'Matches')
        await expect(page.getByRole('button', { name: 'Refresh suggestions' })).toBeEnabled()
        aborted = false // Ignore StrictMode's initial effect cleanup.
        await page.route('**/api/v1/admin/match-suggestions', route => route.fulfill({ status, json: { error: { message: 'Authorization lost' } } }))
        await page.getByRole('button', { name: 'Refresh suggestions' }).click()
        await expect.poll(() => aborted).toBe(true)
        release()
        await expect(summary(page)).toHaveCount(0)
        expect(logCalls(state.calls)).toHaveLength(0)
    })

    for (const path of ['job-runs', 'users']) {
        test(`Settings ${path} authorization loss ${status} clears diagnostics`, async ({ page }) => {
            const state = await mock(page, { enabled: true })
            state.entries = [record]
            let release!: () => void
            const gate = new Promise<void>(resolve => { release = resolve })
            await page.route(`**/api/v1/admin/${path}`, async route => {
                await gate
                await route.fulfill({ status, json: { error: { message: 'Authorization lost' } } }).catch(() => undefined)
            })
            await page.goto('/settings')
            await activity(page)
            await summary(page).click()
            await expect(page.getByRole('cell', { name: 'igdb', exact: true })).toBeVisible()
            release()
            await expect(summary(page)).toHaveCount(0)
            await expect(page.getByRole('region', { name: 'Development connection records' })).toHaveCount(0)
            expect(logCalls(state.calls)).toHaveLength(1)
        })
    }
}

test('Settings non-authentication action failure keeps displayed diagnostics', async ({ page }) => {
    const state = await mock(page, { enabled: true })
    state.entries = [record]
    await page.goto('/settings')
    await activity(page)
    await summary(page).click()
    await expect(page.getByRole('cell', { name: 'igdb', exact: true })).toBeVisible()
    await page.route('**/api/v1/admin/match-suggestions', route => route.fulfill({ status: 503, json: { error: { message: 'Service unavailable' } } }))
    await tab(page, 'Matches')
    await page.getByRole('button', { name: 'Refresh suggestions' }).click()
    await expect(page.getByRole('alert')).toHaveText('Service unavailable')
    await activity(page)
    await expect(page.getByRole('cell', { name: 'igdb', exact: true })).toBeVisible()
    expect(logCalls(state.calls)).toHaveLength(1)
})

for (const path of ['/settings']) {
    for (const enabled of [undefined, false]) {
        test(`${path}: hidden when capability is ${enabled}`, async ({ page }) => {
            const state = await mock(page, { enabled })
            await page.goto(path)
            if (path === '/settings') await activity(page)
            await expect(page.getByRole('heading', { level: 1 })).toBeVisible()
            await expect.poll(() => state.calls.includes('GET /admin/status')).toBe(true)
            await expect(summary(page)).toHaveCount(0)
            expect(logCalls(state.calls)).toHaveLength(0)
        })
    }
    test(`${path}: collapsed, manual empty/entries refresh, safe failure and retry`, async ({ page }) => {
        const state = await mock(page, { enabled: true })
        await page.goto(path)
        if (path === '/settings') await activity(page)
        await expect(summary(page)).toBeVisible()
        expect(logCalls(state.calls)).toHaveLength(0)
        expect(state.calls.filter(call => call === 'GET /admin/status').length).toBeLessThanOrEqual(2) // StrictMode may cancel its first read.
        await summary(page).focus()
        await page.keyboard.press('Enter')
        await expect(page.getByText('No connection diagnostics recorded yet.', { exact: false })).toBeVisible()
        state.entries = [record, { ...record, seq: 2, target: 'tmdb', stage: 'check', outcome: 'success', code: 'connected', httpStatus: undefined, durationMs: 0 }]
        await page.getByRole('button', { name: 'Refresh logs' }).click()
        const rows = page.getByRole('region', { name: 'Development connection records' }).getByRole('row')
        await expect(rows).toHaveCount(3)
        await expect(rows.nth(1).getByRole('cell')).toHaveText(['1', /.+/, 'igdb', 'oauth', 'failure', 'http_status', '401', '24 ms'])
        await expect(rows.nth(1).locator('time')).toHaveAttribute('datetime', record.time)
        await expect(rows.nth(2)).toContainText('—')
        await expect(page.getByText('Restarting Requestarr clears this history.', { exact: false })).toBeVisible()
        state.logStatus = 500
        await page.getByRole('button', { name: 'Refresh logs' }).click()
        await expect(page.getByText('Could not load development connection logs. Try Refresh logs again.')).toBeVisible()
        await expect(page.getByText('RAW_ERROR_SECRET_MUST_NOT_RENDER')).toHaveCount(0)
        await expect(page.getByRole('region', { name: 'Development connection records' })).toHaveCount(0)
        state.logStatus = 200
        await page.getByRole('button', { name: 'Refresh logs' }).click()
        await expect(rows).toHaveCount(3)
        const count = logCalls(state.calls).length
        await page.waitForTimeout(150)
        expect(logCalls(state.calls)).toHaveLength(count)
        expect(await page.evaluate(() => ({ local: localStorage.length, session: sessionStorage.length }))).toEqual({ local: 0, session: 0 })
        await summary(page).click()
        await expect(page.getByRole('region', { name: 'Development connection records' })).toHaveCount(0)
    })
}

for (const role of ['viewer', 'requester']) {
    test(`nonadmin ${role} cannot discover/read logs on either route`, async ({ page }) => {
        const state = await mock(page, { enabled: true, role })
        for (const path of ['/get-started', '/settings']) {
            await page.goto(path)
            await expect(page.getByRole('button', { name: 'Sign out' })).toBeVisible()
            await expect(summary(page)).toHaveCount(0)
        }
        expect(state.calls.some(call => call.includes('/admin/status') || call.includes('/admin/development-logs'))).toBe(false)
    })
}

for (const status of [401, 403]) {
    test(`authorization loss ${status} clears records and stops refresh`, async ({ page }) => {
        const state = await mock(page, { enabled: true })
        state.entries = [record]
        await page.goto('/settings')
        await activity(page)
        await summary(page).click()
        await expect(page.getByRole('cell', { name: 'igdb', exact: true })).toBeVisible()
        state.logStatus = status
        await page.getByRole('button', { name: 'Refresh logs' }).click()
        await expect(summary(page)).toHaveCount(0)
        await expect(page.getByRole('cell', { name: 'igdb', exact: true })).toHaveCount(0)
        expect(logCalls(state.calls)).toHaveLength(2)
    })
}

test('bounded history is text only, even for unexpected markup', async ({ page }) => {
    const state = await mock(page, { enabled: true })
    state.entries = Array.from({ length: 205 }, (_, index) => ({ ...record, seq: index + 1 }))
    state.entries[204].code = '<img src=x onerror=alert(1)>' as DevelopmentLogEntry['code']
    await page.goto('/settings')
    await activity(page)
    await summary(page).click()
    const region = page.getByRole('region', { name: 'Development connection records' })
    await expect(region.getByRole('row')).toHaveCount(201)
    await expect(region.getByRole('row').nth(1).getByRole('cell').first()).toHaveText('6')
    await expect(region.getByText('<img src=x onerror=alert(1)>')).toBeVisible()
    await expect(region.locator('img')).toHaveCount(0)
})

test('closing cancels an in-flight read and a late result cannot replace a newer read', async ({ page }) => {
    const state = await mock(page, { enabled: true })
    let release!: () => void
    const gate = new Promise<void>(resolve => { release = resolve })
    state.logs = async route => { await gate; await route.fulfill({ json: { entries: [record], capacity: 200 } }).catch(() => undefined) }
    await page.goto('/settings')
    await activity(page)
    await summary(page).click()
    await expect(page.getByRole('button', { name: 'Refresh logs' })).toBeDisabled()
    await expect.poll(() => logCalls(state.calls).length).toBe(1)
    await summary(page).click()
    state.logs = null
    await summary(page).click()
    await expect(page.getByText('No connection diagnostics recorded yet.', { exact: false })).toBeVisible()
    release()
    await expect(page.getByRole('cell', { name: 'igdb', exact: true })).toHaveCount(0)
})

test('unmount cancels pending capability and logs reads; remount starts empty', async ({ page }) => {
    const state = await mock(page, { enabled: true })
    let releaseStatus!: () => void
    const statusGate = new Promise<void>(resolve => { releaseStatus = resolve })
    state.status = async route => { await statusGate; await route.fulfill({ json: { developmentLogsEnabled: true } }).catch(() => undefined) }
    await page.goto('/settings')
    await activity(page)
    await expect.poll(() => state.calls.includes('GET /admin/status')).toBe(true)
    await page.getByRole('link', { name: 'Notifications', exact: true }).click()
    releaseStatus()
    await expect(summary(page)).toHaveCount(0)
    state.status = null
    let releaseLogs!: () => void
    const logsGate = new Promise<void>(resolve => { releaseLogs = resolve })
    state.logs = async route => { await logsGate; await route.fulfill({ json: { entries: [record], capacity: 200 } }).catch(() => undefined) }
    await page.getByRole('link', { name: 'Settings', exact: true }).click()
    await activity(page)
    await summary(page).click()
    await expect.poll(() => logCalls(state.calls).length).toBe(1)
    await page.getByRole('link', { name: 'Notifications', exact: true }).click()
    releaseLogs()
    state.logs = null
    await page.getByRole('link', { name: 'Settings', exact: true }).click()
    await activity(page)
    await expect(summary(page)).toBeVisible()
    await expect(page.getByRole('button', { name: 'Refresh logs' })).toHaveCount(0)
    await summary(page).click()
    await expect(page.getByText('No connection diagnostics recorded yet.', { exact: false })).toBeVisible()
})

test('capability lookup failure leaves the optional diagnostics panel hidden', async ({ page }) => {
    const state = await mock(page, { statusCode: 503 })
    await page.goto('/settings')
    await activity(page)
    await expect(page.getByRole('heading', { name: 'Settings', level: 1 })).toBeVisible()
    await expect(summary(page)).toHaveCount(0)
    expect(logCalls(state.calls)).toHaveLength(0)
})
