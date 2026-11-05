import AxeBuilder from '@axe-core/playwright'
import { expect, test, type Page } from '@playwright/test'

type Conn = { slug: string; name: string; url: string; urlFromEnvironment: boolean; urlEnv: string; saved: boolean; takesUsername: boolean; username: string; active: boolean; missing: string[]; credentials: { secret: string; label: string; configured: boolean; source: string; unreadable: boolean }[] }

const conn = (slug: string, name: string, secret: string, label: string, extra: Partial<Conn> = {}): Conn => ({
    slug, name, url: '', urlFromEnvironment: false, urlEnv: `REQUESTARR_${slug.toUpperCase()}_URL`, saved: false, takesUsername: false, username: '', active: false, missing: [],
    credentials: [{ secret, label, configured: false, source: 'none', unreadable: false }], ...extra,
})

async function openSettings(page: Page, conns: Conn[], failWith?: { status: number; code: string; message: string }) {
    const calls: { method: string; path: string; body?: any }[] = []
    const secretNames = [['tmdb_read_token', 'TMDb read token'], ...conns.flatMap(c => c.credentials.map(k => [k.secret, k.label]))]
    await page.route('**/api/v1/**', route => {
        const request = route.request(), path = new URL(request.url()).pathname.replace('/api/v1', '')
        if (path === '/auth/me') return route.fulfill({ json: { user: { id: 'u1', role: 'admin', displayName: 'Admin' }, csrfToken: 'csrf' } })
        if (path === '/admin/secrets') return route.fulfill({ json: { enabled: true, secrets: secretNames.map(([name, label]) => { const k = conns.flatMap(c => c.credentials).find(item => item.secret === name); return { name, label, envVar: 'X', configured: !!k?.configured, source: k?.source ?? 'none', unreadable: false, pendingRestart: false } }) } })
        if (path.startsWith('/admin/secrets/') && request.method() === 'PUT') {
            const c = conns.find(item => item.credentials.some(k => k.secret === path.split('/').pop()))
            if (c) { c.credentials[0].configured = true; c.credentials[0].source = 'stored'; if (c.url) { c.active = true; c.missing = [] } }
            return route.fulfill({ status: 204, body: '' })
        }
        if (path === '/admin/library-connections') return route.fulfill({ json: { connections: conns } })
        const match = path.match(/^\/admin\/library-connections\/(\w+)(\/test)?$/)
        if (match) {
            const c = conns.find(item => item.slug === match[1])!
            const body = request.postDataJSON?.() ?? undefined
            calls.push({ method: request.method(), path, body })
            if (match[2]) return route.fulfill({ json: c.url ? { ok: true, code: 'connected', message: `${c.name} answered with the saved address and credentials.` } : { ok: false, code: 'incomplete_settings', message: 'Enter an address first.' } })
            if (request.method() === 'PUT') {
                if (failWith) return route.fulfill({ status: failWith.status, json: { error: { code: failWith.code, message: failWith.message, requestId: 'r1' } } })
                if (body.url !== undefined) Object.assign(c, { url: body.url, username: body.username ?? '', saved: true })
                if (body.credentials) Object.assign(c.credentials[0], { configured: true, source: 'stored' })
                c.missing = c.credentials[0].configured ? [] : ['token']; c.active = !!c.url && c.credentials[0].configured
            }
            if (request.method() === 'DELETE') Object.assign(c, { url: '', username: '', saved: false, active: false, missing: [] })
            return route.fulfill({ json: c })
        }
        if (path === '/admin/status') return route.fulfill({ json: { providers: [], sources: [], recentErrors: [] } })
        if (path === '/admin/job-runs') return route.fulfill({ json: { sources: [] } })
        if (path === '/admin/users') return route.fulfill({ json: { users: [] } })
        if (path === '/admin/match-suggestions') return route.fulfill({ json: { items: [] } })
        return route.fulfill({ json: { configured: false, enabled: false } })
    })
    await page.goto('/settings')
    return calls
}

const open = (item: import('@playwright/test').Locator) => item.getByRole('button', { name: /^(Settings|Set up)$/ }).click()

const set = () => [
    conn('jellyfin', 'Jellyfin', 'jellyfin_api_key', 'Jellyfin API key', { url: 'https://jf.example.test', urlFromEnvironment: true, active: true, credentials: [{ secret: 'jellyfin_api_key', label: 'Jellyfin API key', configured: true, source: 'environment', unreadable: false }] }),
    conn('plex', 'Plex', 'plex_token', 'Plex token'),
    conn('romm', 'RomM', 'romm_token', 'RomM token'),
    conn('navidrome', 'Navidrome', 'navidrome_password', 'Navidrome password', { takesUsername: true }),
]

test('Settings lists each library server and locks an address set by the environment', async ({ page }) => {
    await openSettings(page, set())
    const list = page.getByRole('list', { name: 'Library connections' })
    await expect(list.getByRole('listitem')).toHaveCount(4)
    const jellyfin = list.getByRole('listitem').filter({ hasText: 'Jellyfin' })
    await expect(jellyfin).toContainText('Syncing · address set by REQUESTARR_JELLYFIN_URL')
    await open(jellyfin)
    await expect(jellyfin.getByLabel('Address')).toBeDisabled()
    await expect(jellyfin.getByRole('button', { name: 'Save and apply' })).toBeDisabled()
    await expect(list.getByRole('listitem').filter({ hasText: 'Plex' })).toContainText('Not set up')
    // Each credential lives only in its own card.
    await expect(page.getByLabel('Plex token')).toHaveCount(1)
})

test('the address and credential are saved together in one request and the secret is not kept', async ({ page }) => {
    const calls = await openSettings(page, set())
    const secretPuts: string[] = []
    page.on('request', r => { if (r.method() === 'PUT' && r.url().includes('/admin/secrets/')) secretPuts.push(r.url()) })
    const plex = page.getByRole('list', { name: 'Library connections' }).getByRole('listitem').filter({ hasText: 'Plex' })
    await open(plex)
    await plex.getByLabel('Address').fill('http://192.168.1.20:32400')
    await plex.getByLabel('Plex token').fill('secret-token')
    await plex.getByRole('button', { name: 'Save and apply' }).click()
    await expect(plex.getByText('Plex saved and applied.')).toBeVisible()
    const puts = calls.filter(c => c.method === 'PUT')
    expect(puts).toHaveLength(1)
    expect(puts[0].body).toEqual({ url: 'http://192.168.1.20:32400', credentials: { plex_token: 'secret-token' } })
    expect(secretPuts).toEqual([])
    await expect(plex).toContainText('Syncing')
    await expect(plex.getByLabel('Plex token')).toHaveValue('')
    await expect(page.getByText('secret-token')).toHaveCount(0)
    expect(await page.evaluate(() => JSON.stringify([localStorage, sessionStorage]))).not.toContain('secret-token')
})

test('an empty credential field keeps the saved credential and is not sent', async ({ page }) => {
    const list = set()
    list[1].credentials[0] = { secret: 'plex_token', label: 'Plex token', configured: true, source: 'stored', unreadable: false }
    const calls = await openSettings(page, list)
    const plex = page.getByRole('list', { name: 'Library connections' }).getByRole('listitem').filter({ hasText: 'Plex' })
    await open(plex)
    await expect(plex.getByLabel('Plex token')).toHaveAttribute('placeholder', /Saved/)
    await plex.getByLabel('Address').fill('http://192.168.1.20:32400')
    await plex.getByRole('button', { name: 'Save and apply' }).click()
    await expect(plex.getByText('Plex saved and applied.')).toBeVisible()
    expect(calls.find(c => c.method === 'PUT')!.body).toEqual({ url: 'http://192.168.1.20:32400' })
    await expect(plex).toContainText('Syncing')
})

test('an environment-locked address stays read-only while the credential is saved', async ({ page }) => {
    const list = set()
    list[0].credentials[0] = { secret: 'jellyfin_api_key', label: 'Jellyfin API key', configured: false, source: 'none', unreadable: false }
    list[0].active = false
    const calls = await openSettings(page, list)
    const jellyfin = page.getByRole('list', { name: 'Library connections' }).getByRole('listitem').filter({ hasText: 'Jellyfin' })
    await open(jellyfin)
    await expect(jellyfin.getByLabel('Address')).toBeDisabled()
    await expect(jellyfin.getByRole('button', { name: 'Save and apply' })).toBeDisabled()
    await jellyfin.getByLabel('Jellyfin API key').fill('jf-key')
    await jellyfin.getByRole('button', { name: 'Save and apply' }).click()
    await expect(jellyfin.getByText('Jellyfin saved and applied.')).toBeVisible()
    expect(calls.find(c => c.method === 'PUT')!.body).toEqual({ credentials: { jellyfin_api_key: 'jf-key' } })
})

test('a failed save says which part failed, nothing is kept, and the page passes axe', async ({ page }) => {
    await openSettings(page, set(), { status: 422, code: 'invalid_secret_value', message: 'the credential was not saved, and nothing was changed: it must be non-empty' })
    const plex = page.getByRole('list', { name: 'Library connections' }).getByRole('listitem').filter({ hasText: 'Plex' })
    await open(plex)
    await plex.getByLabel('Address').fill('http://192.168.1.20:32400')
    await plex.getByLabel('Plex token').fill('secret-token')
    await plex.getByRole('button', { name: 'Save and apply' }).click()
    await expect(plex.getByRole('alert')).toContainText('the credential was not saved, and nothing was changed')
    await expect(plex.getByLabel('Plex token')).toHaveValue('')
    await expect(plex.getByLabel('Address')).toHaveValue('http://192.168.1.20:32400')
    await expect(plex).not.toContainText('saved and applied')
    const results = await new AxeBuilder({ page }).include('[aria-label="Library connections"] > li:nth-child(2)').withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze()
    expect(results.violations.map(v => `${v.id}: ${v.nodes.map(n => n.target.join(' ')).join(' | ')}`)).toEqual([])
})

test('a library can be connected from the keyboard alone', async ({ page }) => {
    const calls = await openSettings(page, set())
    const plex = page.getByRole('list', { name: 'Library connections' }).getByRole('listitem').filter({ hasText: 'Plex' })
    await plex.getByRole('button', { name: /^(Settings|Set up)$/ }).focus()
    await page.keyboard.press('Enter')
    await plex.getByLabel('Address').focus()
    await page.keyboard.type('http://192.168.1.20:32400')
    await page.keyboard.press('Tab')
    await expect(plex.getByLabel('Plex token')).toBeFocused()
    await page.keyboard.type('kb-token')
    await page.keyboard.press('Enter')
    await expect(plex.getByText('Plex saved and applied.')).toBeVisible()
    expect(calls.find(c => c.method === 'PUT')!.body).toEqual({ url: 'http://192.168.1.20:32400', credentials: { plex_token: 'kb-token' } })
    await page.keyboard.press('Shift+Tab')
})

test('Navidrome takes a username, and a saved connection can be tested and removed', async ({ page }) => {
    const calls = await openSettings(page, set())
    const navidrome = page.getByRole('list', { name: 'Library connections' }).getByRole('listitem').filter({ hasText: 'Navidrome' })
    await open(navidrome)
    await expect(navidrome.getByRole('button', { name: 'Test connection' })).toBeDisabled()
    await navidrome.getByLabel('Address').fill('https://music.example.test')
    await navidrome.getByLabel('Username').fill('alice')
    await navidrome.getByRole('button', { name: 'Save and apply' }).click()
    await expect(navidrome.getByText('Navidrome saved and applied.')).toBeVisible()
    expect(calls.find(c => c.method === 'PUT')!.body).toEqual({ url: 'https://music.example.test', username: 'alice' })
    await navidrome.getByRole('button', { name: 'Test connection' }).click()
    await expect(navidrome.getByText('Navidrome answered with the saved address and credentials.')).toBeVisible()
    await navidrome.getByRole('button', { name: 'Remove' }).click()
    await expect(navidrome.getByText('Navidrome removed.')).toBeVisible()
    await expect(navidrome).toContainText('Not set up')
    await expect(navidrome.getByLabel('Address')).toHaveValue('')
})
