import { expect, test } from '@playwright/test'
import { capabilitiesFor } from './capabilities'

const secretList = [
    ['tmdb_read_token', 'TMDb read token'], ['igdb_client_secret', 'IGDB client secret'], ['apprise_config_key', 'Apprise configuration key'],
    ['navidrome_password', 'Navidrome password'], ['jellyfin_api_key', 'Jellyfin API key'], ['plex_token', 'Plex token'], ['romm_token', 'RomM token'],
]

test('Settings exposes a field for every stored secret', async ({ page }) => {
    await page.route('**/api/v1/**', route => {
        const path = new URL(route.request().url()).pathname
        if (path.endsWith('/auth/me')) return route.fulfill({ json: { user: { id: 'u1', role: 'superuser', email: 'a@example.test', displayName: 'Admin' }, capabilities: capabilitiesFor('superuser'), csrfToken: 'csrf' } })
        if (path.endsWith('/admin/secrets')) return route.fulfill({ json: { enabled: true, secrets: secretList.map(([name, label]) => ({ name, label, envVar: 'X', configured: false, source: 'none', unreadable: false, pendingRestart: false })) } })
        if (path.endsWith('/admin/status')) return route.fulfill({ json: { providers: [{ key: 'tmdb', name: 'TMDB', configured: false }, { key: 'igdb', name: 'IGDB', configured: false }, { key: 'apprise', name: 'Apprise', configured: false }], sources: [], recentErrors: [] } })
        if (path.endsWith('/admin/library-connections')) return route.fulfill({ json: { connections: [['jellyfin', 'Jellyfin', 'jellyfin_api_key', 'Jellyfin API key'], ['plex', 'Plex', 'plex_token', 'Plex token'], ['romm', 'RomM', 'romm_token', 'RomM token'], ['navidrome', 'Navidrome', 'navidrome_password', 'Navidrome password']].map(([slug, name, secret, label]) => ({ slug, name, url: '', urlFromEnvironment: false, urlEnv: 'X', saved: false, takesUsername: slug === 'navidrome', username: '', active: false, missing: [], credentials: [{ secret, label, configured: false, source: 'none', unreadable: false }] })) } })
        if (path.endsWith('/admin/job-runs')) return route.fulfill({ json: { sources: [] } })
        if (path.endsWith('/admin/users')) return route.fulfill({ json: { users: [] } })
        if (path.endsWith('/admin/match-suggestions')) return route.fulfill({ json: { items: [] } })
        return route.fulfill({ json: { configured: false, enabled: false } })
    })
    await page.goto('/settings')
    // Every credential sits in its integration's card, behind its Set up button.
    const buttons = page.getByRole('button', { name: 'Set up' })
    await expect(buttons).toHaveCount(7)
    for (let i = 6; i >= 0; i--) await buttons.nth(i).click()
    for (const label of ['TMDb read token', 'IGDB client secret', 'Apprise configuration key', 'Jellyfin API key', 'Plex token', 'RomM token', 'Navidrome password']) {
        await expect(page.getByLabel(label).first()).toBeVisible()
    }
})

async function mockIgdb(page: import('@playwright/test').Page, igdb: { clientId: string; fromEnvironment: boolean }) {
    const puts: { body: any; csrf?: string }[] = []
    await page.route('**/api/v1/**', route => {
        const request = route.request()
        const path = new URL(request.url()).pathname
        if (path.endsWith('/auth/me')) return route.fulfill({ json: { user: { id: 'u1', role: 'superuser', email: 'a@example.test', displayName: 'Admin' }, capabilities: capabilitiesFor('superuser'), csrfToken: 'csrf' } })
        if (path.endsWith('/admin/settings/igdb')) {
            if (request.method() === 'PUT') { puts.push({ body: request.postDataJSON(), csrf: request.headers()['x-csrf-token'] }); igdb = { clientId: request.postDataJSON().clientId, fromEnvironment: false }; return route.fulfill({ json: igdb }) }
            return route.fulfill({ json: igdb })
        }
        if (path.endsWith('/admin/status')) return route.fulfill({ json: { providers: [{ key: 'tmdb', name: 'TMDB', configured: false }, { key: 'igdb', name: 'IGDB', configured: false }], sources: [], recentErrors: [] } })
        if (path.endsWith('/admin/secrets')) return route.fulfill({ json: { enabled: true, secrets: [{ name: 'igdb_client_secret', label: 'IGDB client secret', envVar: 'X', configured: false, source: 'none', unreadable: false, pendingRestart: false }] } })
        if (path.endsWith('/admin/library-connections')) return route.fulfill({ json: { connections: [] } })
        if (path.endsWith('/admin/job-runs')) return route.fulfill({ json: { sources: [] } })
        if (path.endsWith('/admin/users')) return route.fulfill({ json: { users: [] } })
        if (path.endsWith('/admin/match-suggestions')) return route.fulfill({ json: { items: [] } })
        return route.fulfill({ json: {} })
    })
    return puts
}

test('the IGDB client ID is a live field beside the IGDB secret and saves with the CSRF token', async ({ page }) => {
    const puts = await mockIgdb(page, { clientId: 'twitch-old', fromEnvironment: false })
    await page.goto('/settings')
    const card = page.getByRole('listitem').filter({ hasText: 'IGDB' })
    await card.getByRole('button', { name: /Hide settings|Set up/ }).first().click()
    const field = card.getByLabel('IGDB client ID')
    await expect(field).toHaveValue('twitch-old')
    await expect(card.getByLabel('IGDB client secret')).toBeVisible()
    await field.fill('twitch-new')
    await card.getByRole('button', { name: 'Save IGDB client ID' }).click()
    await expect(card.getByRole('status')).toContainText('saved and applied')
    expect(puts).toEqual([{ body: { clientId: 'twitch-new' }, csrf: 'csrf' }])
})

test('an IGDB client ID set by the environment is shown read-only', async ({ page }) => {
    const puts = await mockIgdb(page, { clientId: 'from-env', fromEnvironment: true })
    await page.goto('/settings')
    const card = page.getByRole('listitem').filter({ hasText: 'IGDB' })
    await card.getByRole('button', { name: /Hide settings|Set up/ }).first().click()
    await expect(card.getByLabel('IGDB client ID')).toHaveValue('from-env')
    await expect(card.getByLabel('IGDB client ID')).toBeDisabled()
    await expect(card.getByText('REQUESTARR_IGDB_CLIENT_ID')).toBeVisible()
    await expect(card.getByRole('button', { name: 'Save IGDB client ID' })).toBeDisabled()
    expect(puts).toEqual([])
})

test('the metadata and notification secrets say saved and applied, with no restart wording', async ({ page }) => {
    const puts: string[] = []
    let failNext = false
    await page.route('**/api/v1/**', route => {
        const request = route.request()
        const path = new URL(request.url()).pathname
        if (path.endsWith('/auth/me')) return route.fulfill({ json: { user: { id: 'u1', role: 'superuser', email: 'a@example.test', displayName: 'Admin' }, capabilities: capabilitiesFor('superuser'), csrfToken: 'csrf' } })
        if (path.includes('/admin/secrets/')) {
            puts.push(request.method() + ' ' + path.split('/').pop())
            if (failNext) return route.fulfill({ status: 500, json: { error: { code: 'settings_not_applied', message: 'the change was saved but could not be applied; the previous configuration is still in use' } } })
            return route.fulfill({ status: 204, body: '' })
        }
        if (path.endsWith('/admin/secrets')) return route.fulfill({ json: { enabled: true, secrets: secretList.slice(0, 3).map(([name, label]) => ({ name, label, envVar: 'X', configured: false, source: 'none', unreadable: false, pendingRestart: false })) } })
        if (path.endsWith('/admin/settings/igdb')) return route.fulfill({ json: { clientId: '', fromEnvironment: false } })
        if (path.endsWith('/admin/status')) return route.fulfill({ json: { providers: [{ key: 'tmdb', name: 'TMDB', configured: false }, { key: 'igdb', name: 'IGDB', configured: false }, { key: 'apprise', name: 'Apprise', configured: false }], sources: [], recentErrors: [] } })
        if (path.endsWith('/admin/library-connections')) return route.fulfill({ json: { connections: [] } })
        if (path.endsWith('/admin/job-runs')) return route.fulfill({ json: { sources: [] } })
        if (path.endsWith('/admin/users')) return route.fulfill({ json: { users: [] } })
        if (path.endsWith('/admin/match-suggestions')) return route.fulfill({ json: { items: [] } })
        return route.fulfill({ json: {} })
    })
    await page.goto('/settings')
    await expect(page.getByText('apply after a restart')).toHaveCount(0)
    const tmdb = page.getByRole('listitem').filter({ hasText: 'TMDB' }).first()
    await tmdb.getByRole('button', { name: /Hide settings|Set up/ }).first().click()
    await expect(tmdb.getByText('applies at once')).toBeVisible()
    await tmdb.getByLabel('TMDb read token').fill('a-token')
    await tmdb.getByRole('button', { name: /^Save/ }).first().click()
    await expect(tmdb.getByRole('status')).toContainText('saved and applied')
    await expect(tmdb).not.toContainText('restart')
    failNext = true
    await tmdb.getByLabel('TMDb read token').fill('another')
    await tmdb.getByRole('button', { name: /^Save/ }).first().click()
    await expect(tmdb.getByRole('alert')).toContainText('could not be applied')
    expect(puts).toEqual(['PUT tmdb_read_token', 'PUT tmdb_read_token'])
})
