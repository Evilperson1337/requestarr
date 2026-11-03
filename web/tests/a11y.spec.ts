import AxeBuilder from '@axe-core/playwright'
import { expect, test, type Page } from '@playwright/test'
import { sourceKinds } from './fixtures'

// Automated WCAG 2.x A/AA audit plus a no-horizontal-scroll check of each main
// page at desktop and phone widths, against a deterministic mocked API.
const widths = [{ name: 'desktop', width: 1280, height: 800 }, { name: 'phone', width: 360, height: 800 }]

const request = {
    ID: '22222222-2222-2222-2222-222222222222', CatalogItemID: 'c1', Title: 'Arrival', MediaType: 'movie', ArtworkURL: '', ArtworkKind: 'poster',
    Status: 'requested', Version: 1, RequestedBy: 'user-1', Note: '', Platforms: { any: true, platformKeys: [] }, CatalogPlatforms: [],
    CreatedAt: '2026-01-01T00:00:00Z', availability: 'unknown', subscribed: true, subscriptionSource: 'requester',
}
const settings = {
    general: { instanceName: 'Requestarr', timezone: 'UTC', publicUrl: 'https://requests.example.test' },
    authentication: { issuer: 'https://id.example.test', clientId: 'requestarr', clientSecretEnv: 'REQUESTARR_OIDC_CLIENT_SECRET' },
    providers: { tmdbEnabled: true, tmdbTokenEnv: 'REQUESTARR_TMDB_READ_TOKEN', musicBrainzEnabled: true, musicBrainzContact: '', igdbEnabled: false, igdbClientId: '', igdbClientSecretEnv: 'REQUESTARR_IGDB_CLIENT_SECRET' },
    libraries: Object.fromEntries(['jellyfin', 'plex', 'romm', 'navidrome'].map(k => [k, { enabled: false, url: '', mediaTypes: [], tokenEnv: k === 'navidrome' ? '' : 'REQUESTARR_X', username: '', passwordEnv: '' }])),
    notifications: { appriseEnabled: false, appriseUrl: '', appriseConfigKeyEnv: 'REQUESTARR_APPRISE_CONFIG_KEY' },
    policy: { autoApproveMediaTypes: [] },
}
const draft = (currentStep: string) => ({
    revision: 1, currentStep, inactive: true, updatedAt: '2026-10-01T12:00:00Z', settings,
    validations: [{ target: 'general', revision: 1, valid: false, code: 'connection_failed', message: 'The service could not be reached.', checkedAt: '2026-10-01T12:00:00Z', expiresAt: '2026-10-01T12:10:00Z' }],
    environment: [{ name: 'REQUESTARR_TMDB_READ_TOKEN', secret: true, configured: false, needsRestart: true }],
    blockers: [{ target: 'general', reason: 'failed' }], completedAt: null,
})

async function mockApp(page: Page, step = 'general') {
    await page.route('**/api/v1/**', route => {
        const path = new URL(route.request().url()).pathname.replace('/api/v1', '')
        const json: Record<string, unknown> = {
            '/setup/status': { complete: true, required: false, wizardComplete: false },
            '/auth/me': { user: { id: 'user-1', role: 'admin', email: 'a@example.test', displayName: 'Admin' }, csrfToken: 'csrf' },
            '/requests': { items: [request], nextCursor: null },
            '/notifications': { items: [{ id: 'n1', title: 'Arrival is available', body: 'Now in Jellyfin', createdAt: '2026-10-01T12:00:00Z' }] },
            '/admin/setup/draft': draft(step), '/admin/setup/source-kinds': sourceKinds,
            '/admin/job-runs': { sources: [] }, '/admin/secrets': { enabled: true, secrets: [{ name: 'plex_token', label: 'Plex token', envVar: 'X', configured: false, source: 'none', unreadable: false, pendingRestart: false }, { name: 'tmdb_read_token', label: 'TMDb read token', envVar: 'X', configured: true, source: 'stored', unreadable: false, pendingRestart: false }] }, '/admin/library-connections': { connections: [{ slug: 'plex', name: 'Plex', url: '', urlFromEnvironment: false, urlEnv: 'REQUESTARR_PLEX_URL', saved: false, takesUsername: false, username: '', active: false, missing: [], credentials: [{ secret: 'plex_token', label: 'Plex token', configured: false, source: 'none', unreadable: false }] }] }, '/admin/users': { users: [] }, '/admin/match-suggestions': { items: [] },
            '/admin/status': { providers: [{ key: 'tmdb', name: 'TMDB', configured: true }], sources: [{ slug: 'jellyfin', name: 'Jellyfin', configured: false, lastRun: null, lastSuccessAt: null, lastSnapshotAt: null, lastError: '' }], recentErrors: [] },
            '/resolve': { Item: { ID: 'c1', MediaType: 'game', Provider: 'igdb', Title: 'Outer Wilds', Year: 2019, ArtworkURL: '', ArtworkKind: 'poster', ExternalURL: '' }, Platforms: [{ providerId: '6', key: 'pc', name: 'PC' }] },
        }
        return route.fulfill(path in json ? { json: json[path] } : { status: 404, json: { error: { code: 'not_found', message: 'unmocked' } } })
    })
}

async function audit(page: Page) {
    const results = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze()
    expect(results.violations.map(v => `${v.id}: ${v.nodes.map(n => n.target.join(' ')).join(' | ')}`)).toEqual([])
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)
    const culprits = overflow > 0 ? await page.evaluate(() => [...document.querySelectorAll('body *')].filter(el => el.getBoundingClientRect().right > document.documentElement.clientWidth + 1).slice(0, 5).map(el => el.tagName.toLowerCase() + '.' + el.className)) : []
    expect(overflow, `horizontal scroll caused by ${culprits.join(', ')}`).toBeLessThanOrEqual(0)
}

const pages: { name: string; step?: string; open: (page: Page) => Promise<void> }[] = [
    { name: 'Home', open: async page => { await page.goto('/'); await expect(page.getByRole('heading', { name: 'Request Something' })).toBeVisible() } },
    { name: 'Request preview dialog', open: async page => {
        await page.goto('/'); await page.getByPlaceholder('Paste a supported link or an ASIN…').fill('https://www.igdb.com/games/outer-wilds')
        await page.getByRole('button', { name: 'Preview' }).click(); await expect(page.getByRole('dialog')).toBeVisible() } },
    { name: 'Requests', open: async page => { await page.goto('/requests'); await expect(page.getByRole('heading', { name: 'Arrival', exact: true })).toBeVisible() } },
    { name: 'Notifications', open: async page => { await page.goto('/notifications'); await expect(page.getByText('Arrival is available')).toBeVisible() } },
    { name: 'Settings', open: async page => { await page.goto('/settings'); await expect(page.getByRole('heading', { name: 'Settings', level: 1 })).toBeVisible(); await page.getByRole('button', { name: 'Set up' }).click(); await expect(page.getByLabel('Address')).toBeVisible() } },
    { name: 'Settings · Activity', open: async page => { await page.goto('/settings'); await page.getByRole('button', { name: 'Activity', exact: true }).click(); await expect(page.getByRole('heading', { name: 'Recent errors' })).toBeVisible() } },
    ...['general', 'authentication', 'providers', 'libraries', 'notifications', 'policy', 'review'].map(step => ({
        name: `Setup wizard · ${step}`, step, open: async (page: Page) => { await page.goto('/setup'); await expect(page.getByLabel('Setup steps')).toBeVisible() },
    })),
]

for (const size of widths) {
    for (const entry of pages) {
        test(`${entry.name} has no axe violations or horizontal scroll at ${size.name} width`, async ({ page }) => {
            await page.setViewportSize({ width: size.width, height: size.height })
            await mockApp(page, entry.step)
            await entry.open(page)
            await audit(page)
        })
    }
}

test('request dialog is keyboard operable: focus enters it, Tab stays inside, Escape closes and returns focus', async ({ page }) => {
    await mockApp(page)
    await page.goto('/')
    await page.getByPlaceholder('Paste a supported link or an ASIN…').fill('https://www.igdb.com/games/outer-wilds')
    await page.keyboard.press('Enter')
    const dialog = page.getByRole('dialog')
    await expect(dialog).toBeVisible()
    const insideDialog = () => page.evaluate(() => !!document.activeElement?.closest('[role="dialog"]'))
    await expect.poll(insideDialog).toBe(true)
    for (let i = 0; i < 12; i++) {
        await page.keyboard.press('Tab')
        expect(await insideDialog(), `focus escaped the dialog after ${i + 1} Tab presses`).toBe(true)
    }
    await page.keyboard.press('Escape')
    await expect(dialog).toHaveCount(0)
    await expect(page.getByPlaceholder('Paste a supported link or an ASIN…')).toBeFocused()
})
