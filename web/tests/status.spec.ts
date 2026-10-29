import { expect, test } from '@playwright/test'

test('admin Settings shows Source outcomes, providers and recent errors', async ({ page }) => {
    await page.route('**/api/v1/**', route => {
        const path = new URL(route.request().url()).pathname
        if (path.endsWith('/setup/status')) return route.fulfill({ json: { complete: true, required: false, wizardComplete: true } })
        if (path.endsWith('/auth/me')) return route.fulfill({ json: { user: { id: 'u1', role: 'admin', email: 'a@example.test', displayName: 'Admin' }, csrfToken: 'csrf' } })
        if (path.endsWith('/admin/status')) return route.fulfill({ json: {
            providers: [{ key: 'tmdb', name: 'TMDB', configured: true }, { key: 'igdb', name: 'IGDB', configured: false }],
            sources: [
                { slug: 'jellyfin', name: 'Jellyfin', configured: true, lastRun: { id: 'r1', sourceId: 's', trigger: 'schedule', status: 'failed', startedAt: '2026-10-24T12:00:00Z', finishedAt: '2026-10-24T12:01:00Z', error: 'library unreachable' }, lastSuccessAt: null, lastSnapshotAt: null, lastError: 'library unreachable' },
                { slug: 'plex', name: 'Plex', configured: false, lastRun: null, lastSuccessAt: null, lastSnapshotAt: null, lastError: '' },
            ],
            recentErrors: [{ source: 'Jellyfin', run: { id: 'r1', sourceId: 's', trigger: 'schedule', status: 'failed', startedAt: '2026-10-24T12:00:00Z', finishedAt: null, error: 'library unreachable' } }],
        } })
        if (path.endsWith('/admin/job-runs')) return route.fulfill({ json: { sources: [] } })
        if (path.endsWith('/admin/users')) return route.fulfill({ json: { users: [] } })
        if (path.endsWith('/admin/match-suggestions')) return route.fulfill({ json: { items: [] } })
        return route.fulfill({ json: {} })
    })
    await page.goto('/settings')
    await expect(page.getByRole('heading', { name: 'Operational status' })).toBeVisible()
    const jellyfin = page.getByRole('row', { name: /Jellyfin/ })
    await expect(jellyfin).toContainText('failed')
    await expect(jellyfin).toContainText('library unreachable')
    await expect(page.getByRole('row', { name: /Plex/ })).toContainText('No')
    await expect(page.getByText('IGDB: Not configured')).toBeVisible()
    await expect(page.getByRole('listitem').filter({ hasText: 'Jellyfin ·' })).toContainText('library unreachable')
})
