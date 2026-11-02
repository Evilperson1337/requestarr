import { expect, test } from '@playwright/test'

const secretList = [
    ['tmdb_read_token', 'TMDb read token'], ['igdb_client_secret', 'IGDB client secret'], ['apprise_config_key', 'Apprise configuration key'],
    ['navidrome_password', 'Navidrome password'], ['jellyfin_api_key', 'Jellyfin API key'], ['plex_token', 'Plex token'], ['romm_token', 'RomM token'],
]

test('post-Finish Settings exposes a field for every stored secret', async ({ page }) => {
    await page.route('**/api/v1/**', route => {
        const path = new URL(route.request().url()).pathname
        if (path.endsWith('/setup/status')) return route.fulfill({ json: { complete: true, required: false, wizardComplete: true } })
        if (path.endsWith('/auth/me')) return route.fulfill({ json: { user: { id: 'u1', role: 'admin', email: 'a@example.test', displayName: 'Admin' }, csrfToken: 'csrf' } })
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
