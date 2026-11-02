import { expect, test } from '@playwright/test'

const run = { id: 'r1', sourceId: 's', trigger: 'schedule', status: 'failed', startedAt: '2026-10-24T12:00:00Z', finishedAt: '2026-10-24T12:01:00Z', error: 'library unreachable' }
const connection = (slug: string, name: string, extra: object = {}) => ({ slug, name, url: '', urlFromEnvironment: false, urlEnv: `REQUESTARR_${slug.toUpperCase()}_URL`, saved: false, takesUsername: false, username: '', active: false, missing: [], credentials: [], ...extra })

test('admin Settings shows each integration with its outcome, and recent errors under Activity', async ({ page }) => {
    await page.route('**/api/v1/**', route => {
        const path = new URL(route.request().url()).pathname
        if (path.endsWith('/setup/status')) return route.fulfill({ json: { complete: true, required: false, wizardComplete: true } })
        if (path.endsWith('/auth/me')) return route.fulfill({ json: { user: { id: 'u1', role: 'admin', email: 'a@example.test', displayName: 'Admin' }, csrfToken: 'csrf' } })
        if (path.endsWith('/admin/status')) return route.fulfill({ json: {
            providers: [{ key: 'tmdb', name: 'TMDB', configured: true }, { key: 'igdb', name: 'IGDB', configured: false }],
            sources: [
                { slug: 'jellyfin', name: 'Jellyfin', configured: true, lastRun: run, lastSuccessAt: '2026-10-23T12:00:00Z', lastSnapshotAt: null, lastError: 'library unreachable' },
                { slug: 'plex', name: 'Plex', configured: false, lastRun: null, lastSuccessAt: null, lastSnapshotAt: null, lastError: '' },
            ],
            recentErrors: [{ source: 'Jellyfin', run }],
        } })
        if (path.endsWith('/admin/library-connections')) return route.fulfill({ json: { connections: [connection('jellyfin', 'Jellyfin', { url: 'https://jf.example.test', active: true }), connection('plex', 'Plex')] } })
        if (path.endsWith('/admin/job-runs')) return route.fulfill({ json: { sources: [] } })
        if (path.endsWith('/admin/users')) return route.fulfill({ json: { users: [] } })
        if (path.endsWith('/admin/match-suggestions')) return route.fulfill({ json: { items: [] } })
        return route.fulfill({ json: {} })
    })
    await page.goto('/settings')
    await expect(page.getByRole('heading', { name: 'Settings', level: 1 })).toBeVisible()
    const list = page.getByRole('list', { name: 'Library connections' })
    const jellyfin = list.getByRole('listitem').filter({ hasText: 'Jellyfin' })
    await expect(jellyfin).toContainText('Syncing')
    await expect(jellyfin).toContainText('failed')
    await expect(jellyfin).toContainText('library unreachable')
    await expect(list.getByRole('listitem').filter({ hasText: 'Plex' })).toContainText('Not set up')
    await expect(page.getByRole('listitem').filter({ hasText: 'IGDB' })).toContainText('Not configured')
    await expect(page.getByRole('listitem').filter({ hasText: 'TMDB' })).toContainText('Configured')
    await page.getByRole('button', { name: /^Activity/ }).click()
    await expect(page.getByRole('listitem').filter({ hasText: 'Jellyfin ·' })).toContainText('library unreachable')
})
