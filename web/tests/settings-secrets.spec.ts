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
        if (path.endsWith('/admin/status')) return route.fulfill({ json: { providers: [], sources: [], recentErrors: [] } })
        if (path.endsWith('/admin/job-runs')) return route.fulfill({ json: { sources: [] } })
        if (path.endsWith('/admin/users')) return route.fulfill({ json: { users: [] } })
        if (path.endsWith('/admin/match-suggestions')) return route.fulfill({ json: { items: [] } })
        return route.fulfill({ json: { configured: false, enabled: false } })
    })
    await page.goto('/settings')
    for (const label of ['TMDb read token', 'IGDB client secret', 'Apprise configuration key', 'Jellyfin API key', 'Plex token', 'RomM token', 'Navidrome password']) {
        await expect(page.getByLabel(label).first()).toBeVisible()
    }
})
