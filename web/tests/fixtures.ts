// The Source kind registry as GET /admin/setup/source-kinds serves it.
export const sourceKinds = {
    kinds: [
        { slug: 'jellyfin', name: 'Jellyfin', urlEnv: 'REQUESTARR_JELLYFIN_URL', mediaTypes: ['movie', 'tv'], credentials: [{ role: 'token', env: 'REQUESTARR_JELLYFIN_API_KEY', secret: 'jellyfin_api_key', label: 'Jellyfin API key' }] },
        { slug: 'plex', name: 'Plex', urlEnv: 'REQUESTARR_PLEX_URL', mediaTypes: ['movie', 'tv', 'album', 'song'], credentials: [{ role: 'token', env: 'REQUESTARR_PLEX_TOKEN', secret: 'plex_token', label: 'Plex token' }] },
        { slug: 'romm', name: 'RomM', urlEnv: 'REQUESTARR_ROMM_URL', mediaTypes: ['game'], credentials: [{ role: 'token', env: 'REQUESTARR_ROMM_TOKEN', secret: 'romm_token', label: 'RomM token' }] },
        { slug: 'navidrome', name: 'Navidrome', urlEnv: 'REQUESTARR_NAVIDROME_URL', mediaTypes: ['album', 'song'], credentials: [{ role: 'username', env: 'REQUESTARR_NAVIDROME_USERNAME' }, { role: 'password', env: 'REQUESTARR_NAVIDROME_PASSWORD', secret: 'navidrome_password', label: 'Navidrome password' }] },
    ],
}

// Lets the page's timers run for `ms` of fake time (the test must have called
// page.clock.install() before page.goto), then makes one round trip through
// the mocked API. Any request a timer started would have reached the route
// handler first, so a "nothing was requested" assertion after this is sound
// without a real sleep.
export async function letTimersRun(page: import('@playwright/test').Page, ms: number) {
    await page.clock.runFor(ms)
    await page.evaluate(() => fetch('/api/v1/search/types', { cache: 'no-store' }).then(() => undefined))
}
