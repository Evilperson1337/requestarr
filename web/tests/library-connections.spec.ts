import { expect, test, type Page } from '@playwright/test'

type Conn = { slug: string; name: string; url: string; urlFromEnvironment: boolean; urlEnv: string; saved: boolean; takesUsername: boolean; username: string; active: boolean; missing: string[]; credentials: { secret: string; label: string; configured: boolean; source: string; unreadable: boolean }[] }

const conn = (slug: string, name: string, secret: string, label: string, extra: Partial<Conn> = {}): Conn => ({
    slug, name, url: '', urlFromEnvironment: false, urlEnv: `REQUESTARR_${slug.toUpperCase()}_URL`, saved: false, takesUsername: false, username: '', active: false, missing: [],
    credentials: [{ secret, label, configured: false, source: 'none', unreadable: false }], ...extra,
})

async function openSettings(page: Page, conns: Conn[]) {
    const calls: { method: string; path: string; body?: any }[] = []
    const secretNames = [['tmdb_read_token', 'TMDb read token'], ...conns.flatMap(c => c.credentials.map(k => [k.secret, k.label]))]
    await page.route('**/api/v1/**', route => {
        const request = route.request(), path = new URL(request.url()).pathname.replace('/api/v1', '')
        if (path === '/setup/status') return route.fulfill({ json: { complete: true, required: false, wizardComplete: true } })
        if (path === '/auth/me') return route.fulfill({ json: { user: { id: 'u1', role: 'admin', displayName: 'Admin' }, csrfToken: 'csrf' } })
        if (path === '/admin/secrets') return route.fulfill({ json: { enabled: true, secrets: secretNames.map(([name, label]) => ({ name, label, envVar: 'X', configured: false, source: 'none', unreadable: false, pendingRestart: false })) } })
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
            if (match[2]) return route.fulfill({ json: c.url ? { ok: true, code: 'connected', message: `${c.name} answered.` } : { ok: false, code: 'incomplete_settings', message: 'Enter an address first.' } })
            if (request.method() === 'PUT') { Object.assign(c, { url: body.url, username: body.username ?? '', saved: true }); c.missing = c.credentials[0].configured ? [] : ['token']; c.active = c.credentials[0].configured }
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

const set = () => [
    conn('jellyfin', 'Jellyfin', 'jellyfin_api_key', 'Jellyfin API key', { url: 'https://jf.example.test', urlFromEnvironment: true, active: true, credentials: [{ secret: 'jellyfin_api_key', label: 'Jellyfin API key', configured: true, source: 'environment', unreadable: false }] }),
    conn('plex', 'Plex', 'plex_token', 'Plex token'),
    conn('romm', 'RomM', 'romm_token', 'RomM token'),
    conn('navidrome', 'Navidrome', 'navidrome_password', 'Navidrome password', { takesUsername: true }),
]

test('Settings lists each library server, locks an address set by the environment and keeps its secret out of the generic list', async ({ page }) => {
    await openSettings(page, set())
    const list = page.getByRole('list', { name: 'Library connections' })
    await expect(list.getByRole('listitem')).toHaveCount(4)
    const jellyfin = list.getByRole('listitem').filter({ hasText: 'Jellyfin' })
    await expect(jellyfin).toContainText('Syncing · address set by REQUESTARR_JELLYFIN_URL')
    await expect(jellyfin.getByLabel('Address')).toBeDisabled()
    await expect(jellyfin.getByRole('button', { name: 'Save and apply' })).toBeDisabled()
    await expect(list.getByRole('listitem').filter({ hasText: 'Plex' })).toContainText('Not set up')
    // Each library credential lives in its own card, not in "Stored secrets" as well.
    await expect(page.getByLabel('Plex token')).toHaveCount(1)
    await expect(page.getByRole('region', { name: 'Stored secrets' }).getByLabel('TMDb read token')).toBeVisible()
})

test('saving an address applies it, and the credential completes it', async ({ page }) => {
    const calls = await openSettings(page, set())
    const plex = page.getByRole('list', { name: 'Library connections' }).getByRole('listitem').filter({ hasText: 'Plex' })
    await plex.getByLabel('Address').fill('http://192.168.1.20:32400')
    await plex.getByRole('button', { name: 'Save and apply' }).click()
    await expect(plex.getByText('Plex saved and applied.')).toBeVisible()
    expect(calls.find(c => c.method === 'PUT')!.body).toEqual({ url: 'http://192.168.1.20:32400' })
    await expect(plex).toContainText('Off · needs token')
    await plex.getByLabel('Plex token').fill('secret-token')
    await plex.getByRole('button', { name: 'Save Plex token' }).click()
    await expect(plex.getByText('Plex token saved and applied.')).toBeVisible()
    await expect(plex).toContainText('Syncing')
})

test('Navidrome takes a username, and a saved connection can be tested and removed', async ({ page }) => {
    const calls = await openSettings(page, set())
    const navidrome = page.getByRole('list', { name: 'Library connections' }).getByRole('listitem').filter({ hasText: 'Navidrome' })
    await expect(navidrome.getByRole('button', { name: 'Test connection' })).toBeDisabled()
    await navidrome.getByLabel('Address').fill('https://music.example.test')
    await navidrome.getByLabel('Username').fill('alice')
    await navidrome.getByRole('button', { name: 'Save and apply' }).click()
    await expect(navidrome.getByText('Navidrome saved and applied.')).toBeVisible()
    expect(calls.find(c => c.method === 'PUT')!.body).toEqual({ url: 'https://music.example.test', username: 'alice' })
    await navidrome.getByRole('button', { name: 'Test connection' }).click()
    await expect(navidrome.getByText('Connected. Navidrome answered.')).toBeVisible()
    await navidrome.getByRole('button', { name: 'Remove' }).click()
    await expect(navidrome.getByText('Navidrome removed.')).toBeVisible()
    await expect(navidrome).toContainText('Not set up')
    await expect(navidrome.getByLabel('Address')).toHaveValue('')
})
