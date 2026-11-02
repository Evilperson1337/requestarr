import { expect, test, type Page } from '@playwright/test'
import AxeBuilder from '@axe-core/playwright'

const kinds = {
    movie: [['video', 'Video'], ['audio', 'Audio'], ['subtitles', 'Subtitles'], ['wrong_item', 'Wrong item'], ['other', 'Other']],
    tv: [['video', 'Video'], ['audio', 'Audio'], ['subtitles', 'Subtitles'], ['wrong_item', 'Wrong item'], ['other', 'Other']],
    album: [['audio', 'Audio'], ['missing_tracks', 'Missing or wrong tracks'], ['wrong_tags', 'Wrong tags or artwork'], ['other', 'Other']],
    game: [['no_boot', "Doesn't boot or launch"], ['wrong_region', 'Wrong region'], ['wrong_version', 'Wrong version or platform'], ['missing_files', 'Missing files'], ['other', 'Other']],
} as const
const item = (id: string, title: string, mediaType: keyof typeof kinds, extra: object = {}) => ({
    id, title, year: 2016, mediaType, source: 'Plex', sourceKind: 'plex', addedAt: '2026-11-01T12:00:00Z', openIssues: 0,
    issueKinds: kinds[mediaType].map(([kind, label]) => ({ kind, label })), ...extra,
})
const issue = (id: string, title: string, extra: object = {}) => ({
    id, libraryItemId: 'a', title, mediaType: 'movie', source: 'Plex', kind: 'subtitles', kindLabel: 'Subtitles', part: {}, status: 'open', reporters: 1,
    goneFromLibrary: false, createdAt: '2026-11-01T12:00:00Z', updatedAt: '2026-11-01T12:00:00Z', ...extra,
})

type Calls = { method: string; path: string; search: string; body?: any }[]

async function mock(page: Page, role: string, handlers: (path: string, url: URL, method: string, body: any) => unknown | undefined = () => undefined) {
    const calls: Calls = []
    await page.route('**/api/v1/**', route => {
        const request = route.request(), url = new URL(request.url()), path = url.pathname.replace('/api/v1', '')
        const body = request.method() === 'GET' ? undefined : request.postDataJSON?.() ?? undefined
        if (path === '/setup/status') return route.fulfill({ json: { complete: true, required: false, wizardComplete: true } })
        if (path === '/auth/me') return route.fulfill({ json: { user: { id: 'u1', role, displayName: 'User' }, csrfToken: 'csrf' } })
        calls.push({ method: request.method(), path, search: url.search, body })
        const custom = handlers(path, url, request.method(), body)
        if (custom !== undefined) return route.fulfill(custom as any)
        if (path === '/library/items') return route.fulfill({ json: { items: [], nextCursor: null } })
        if (path === '/issues/summary') return route.fulfill({ json: { open: 0 } })
        if (path === '/issues') return route.fulfill({ json: { items: [], nextCursor: null } })
        return route.fulfill({ json: { items: [], nextCursor: null } })
    })
    return calls
}

test('the Library lists one media type at a time and loads more on request', async ({ page }) => {
    const calls = await mock(page, 'viewer', (path, url) => {
        if (path !== '/library/items') return undefined
        const type = url.searchParams.get('type') as keyof typeof kinds
        if (type === 'movie' && !url.searchParams.get('cursor')) return { json: { items: [item('a', 'Arrival', 'movie', { artworkUrl: '/api/v1/library/items/a/artwork' })], nextCursor: 'c2' } }
        if (type === 'movie') return { json: { items: [item('b', 'Blade Runner', 'movie')], nextCursor: null } }
        return { json: { items: [item('g', 'Hades II', 'game', { year: undefined, detail: 'Windows', source: 'RomM', sourceKind: 'romm' })], nextCursor: null } }
    })
    await page.goto('/library')
    const grid = page.getByRole('list', { name: 'Movies' })
    await expect(grid.getByRole('button', { name: 'Arrival 2016 · Plex' })).toBeVisible()
    await page.getByRole('button', { name: 'Show more' }).click()
    await expect(grid.getByRole('listitem')).toHaveCount(2)
    expect(calls.map(c => c.search).filter(s => s.includes('cursor'))).toEqual(['?type=movie&limit=48&cursor=c2'])
    await page.getByRole('button', { name: 'Games', exact: true }).click()
    await expect(page.getByRole('list', { name: 'Games' }).getByRole('button', { name: /Hades II/ })).toBeVisible()
    await expect(page.locator('.railCover.game')).toHaveCount(1)
    await expect(page.getByRole('list', { name: 'Games' })).toContainText('Windows · RomM')
})

test('search and the library filter are sent to the server', async ({ page }) => {
    const calls = await mock(page, 'viewer')
    await page.goto('/library')
    await page.getByLabel('Search the library').fill('arrival')
    await page.getByLabel('Library', { exact: true }).selectOption('plex')
    await expect.poll(() => calls.some(c => c.search.includes('q=arrival') && c.search.includes('source=plex'))).toBe(true)
    await expect(page.getByText('Nothing matches that.')).toBeVisible()
})

test('anyone can report an issue with an item; Other needs words; the kinds fit the media type', async ({ page }) => {
    const calls = await mock(page, 'viewer', (path, _url, method) => {
        if (path === '/library/items') return { json: { items: [item('a', 'Arrival', 'movie', { link: 'https://plex.example/web#!/x', openIssues: 2 })], nextCursor: null } }
        if (path === '/issues' && method === 'POST') return { status: 201, json: { issue: issue('i1', 'Arrival'), joined: false } }
        return undefined
    })
    await page.goto('/library')
    await page.getByRole('button', { name: /Arrival/ }).click()
    const dialog = page.getByRole('dialog', { name: /Arrival/ })
    await expect(dialog.getByRole('link', { name: 'Open in Plex' })).toHaveAttribute('href', 'https://plex.example/web#!/x')
    await expect(dialog.getByRole('status')).toHaveText('2 open issues reported for this item.')
    await dialog.getByRole('button', { name: 'Report an issue' }).click()
    await expect(dialog.getByRole('radio')).toHaveCount(5)
    await dialog.getByRole('radio', { name: 'Other' }).check()
    await expect(dialog.getByRole('button', { name: 'Send report' })).toBeDisabled()
    await dialog.getByRole('radio', { name: 'Subtitles' }).check()
    await dialog.getByLabel('Details (optional)').fill('out of sync')
    await dialog.getByRole('button', { name: 'Send report' }).click()
    await expect(dialog.getByText('Thanks, your report was sent.')).toBeVisible()
    expect(calls.find(c => c.method === 'POST')!.body).toEqual({ libraryItemId: 'a', kind: 'subtitles', description: 'out of sync' })
})

test('a series report can name a season and episode, a game offers game kinds, and joining says so', async ({ page }) => {
    const calls = await mock(page, 'requester', (path, url, method) => {
        if (path === '/library/items') return { json: { items: [url.searchParams.get('type') === 'game' ? item('g', 'Hades II', 'game') : item('s', 'Severance', 'tv')], nextCursor: null } }
        if (path === '/issues' && method === 'POST') return { status: 200, json: { issue: issue('i1', 'Severance'), joined: true } }
        return undefined
    })
    await page.goto('/library')
    await page.getByRole('button', { name: 'TV', exact: true }).click()
    await page.getByRole('button', { name: /Severance/ }).click()
    const dialog = page.getByRole('dialog')
    await dialog.getByRole('button', { name: 'Report an issue' }).click()
    await expect(dialog.getByLabel('Episode (optional)')).toBeDisabled()
    await dialog.getByLabel('Season (optional)').fill('2')
    await dialog.getByLabel('Episode (optional)').fill('4')
    await dialog.getByRole('radio', { name: 'Audio' }).check()
    await dialog.getByRole('button', { name: 'Send report' }).click()
    await expect(dialog.getByText('Added to the existing report.')).toBeVisible()
    expect(calls.find(c => c.method === 'POST')!.body).toEqual({ libraryItemId: 's', kind: 'audio', part: { episodes: [{ season: 2, episode: 4 }] } })
    await dialog.getByRole('button', { name: 'Done' }).click()
    await page.getByRole('button', { name: 'Games', exact: true }).click()
    await page.getByRole('button', { name: /Hades II/ }).click()
    await page.getByRole('dialog').getByRole('button', { name: 'Report an issue' }).click()
    await expect(page.getByRole('dialog').getByRole('radio', { name: 'Wrong region' })).toBeVisible()
    await expect(page.getByRole('dialog').getByRole('radio', { name: 'Subtitles' })).toHaveCount(0)
})

test('hitting the report limit is explained', async ({ page }) => {
    await mock(page, 'viewer', (path, _u, method) => {
        if (path === '/library/items') return { json: { items: [item('a', 'Arrival', 'movie')], nextCursor: null } }
        if (path === '/issues' && method === 'POST') return { status: 429, json: { error: { code: 'rate_limited', message: 'slow down' } } }
        return undefined
    })
    await page.goto('/library')
    await page.getByRole('button', { name: /Arrival/ }).click()
    await page.getByRole('button', { name: 'Report an issue' }).click()
    await page.getByRole('button', { name: 'Send report' }).click()
    await expect(page.getByRole('alert')).toContainText('You have reported a lot of issues recently')
})

test('only administrators get an open-issue count in the sidebar', async ({ page }) => {
    await mock(page, 'admin', path => (path === '/issues/summary' ? { json: { open: 3 } } : undefined))
    await page.goto('/')
    await expect(page.getByRole('link', { name: /^Issues/ })).toContainText('3')
    const viewer = await page.context().newPage()
    await mock(viewer, 'viewer', path => (path === '/issues/summary' ? { json: { open: 3 } } : undefined))
    await viewer.goto('/')
    await expect(viewer.getByRole('link', { name: 'Issues', exact: true })).toBeVisible()
})

test('the Issues list shows status and part, filters by status, and links to the detail', async ({ page }) => {
    const calls = await mock(page, 'viewer', path => path === '/issues' ? { json: { items: [issue('i1', 'Severance', { kind: 'audio', kindLabel: 'Audio', part: { seasons: [2] }, reporters: 2, goneFromLibrary: true })], nextCursor: null } } : undefined)
    await page.goto('/issues')
    const row = page.getByRole('link', { name: /Severance/ })
    await expect(row).toContainText('Audio · Season 2')
    await expect(row).toContainText('2 reporters')
    await expect(row).toContainText('No longer in the library')
    await expect(row).toHaveAttribute('href', '/issues?issueId=i1')
    await page.getByRole('button', { name: 'Resolved' }).click()
    await expect.poll(() => calls.some(c => c.path === '/issues' && c.search.includes('status=resolved'))).toBe(true)
})

test('an administrator reads the timeline and resolves or reopens an issue', async ({ page }) => {
    let status = 'open'
    const calls = await mock(page, 'admin', (path, _u, method) => {
        if (path === '/issues/i1' && method === 'GET') return { json: { issue: issue('i1', 'Arrival', { status }), youReported: false, reporters: [{ userId: 'r1', name: 'Sam', note: 'late', at: '2026-11-01T12:00:00Z', left: false }], events: [{ id: 'e1', type: 'reported', actorName: 'Sam', comment: 'late', at: '2026-11-01T12:00:00Z' }] } }
        if (path === '/issues/i1/status') { status = 'resolved'; return { json: { issue: issue('i1', 'Arrival', { status }) } } }
        return undefined
    })
    await page.goto('/issues?issueId=i1')
    await expect(page.getByRole('heading', { name: 'Arrival', level: 1 })).toBeVisible()
    await expect(page.getByRole('list').filter({ hasText: 'Sam reported this' })).toBeVisible()
    await expect(page.getByText('Sam: late')).toBeVisible()
    await page.getByLabel('Closing note (optional)').fill('re-synced')
    await page.getByRole('button', { name: 'Resolve' }).click()
    await expect(page.getByRole('button', { name: 'Reopen' })).toBeVisible()
    expect(calls.find(c => c.path === '/issues/i1/status')!.body).toEqual({ status: 'resolved', comment: 're-synced' })
})

test('a reporter can comment and stop following but cannot decide; a stranger gets a plain not-found', async ({ page }) => {
    const calls = await mock(page, 'requester', (path, _u, method) => {
        if (path === '/issues/i1' && method === 'GET') return { json: { issue: issue('i1', 'Arrival'), youReported: true, reporters: [], events: [] } }
        if (path === '/issues/nope') return { status: 404, json: { error: { code: 'issue_not_found', message: 'no' } } }
        return { status: 204, body: '' }
    })
    await page.goto('/issues?issueId=i1')
    await expect(page.getByRole('button', { name: 'Resolve' })).toHaveCount(0)
    await page.getByLabel('Add a comment').fill('still late')
    await page.getByRole('button', { name: 'Comment' }).click()
    await expect.poll(() => calls.some(c => c.path === '/issues/i1/comments' && c.body.text === 'still late')).toBe(true)
    await page.getByRole('button', { name: 'Stop following' }).click()
    await expect.poll(() => calls.some(c => c.method === 'DELETE' && c.path === '/issues/i1/reporter')).toBe(true)
    await page.goto('/issues?issueId=nope')
    await expect(page.getByRole('heading', { name: 'Issue not found' })).toBeVisible()
})

for (const [name, open] of [
    ['Library', async (page: Page) => { await page.goto('/library'); await expect(page.getByRole('button', { name: /Arrival/ })).toBeVisible() }],
    ['Library report dialog', async (page: Page) => { await page.goto('/library'); await page.getByRole('button', { name: /Arrival/ }).click(); await page.getByRole('button', { name: 'Report an issue' }).click(); await expect(page.getByRole('radio').first()).toBeVisible() }],
    ['Issues', async (page: Page) => { await page.goto('/issues'); await expect(page.getByRole('link', { name: /Severance/ })).toBeVisible() }],
    ['Issue detail', async (page: Page) => { await page.goto('/issues?issueId=i1'); await expect(page.getByRole('heading', { name: 'Arrival', level: 1 })).toBeVisible() }],
] as const) {
    for (const size of [{ width: 1440, height: 900 }, { width: 390, height: 844 }]) {
        test(`${name} has no axe violations or horizontal scroll at ${size.width}px`, async ({ page }) => {
            await page.setViewportSize(size)
            await mock(page, 'admin', (path, _u, method) => {
                if (path === '/library/items') return { json: { items: [item('a', 'Arrival', 'movie')], nextCursor: null } }
                if (path === '/issues' && method === 'GET') return { json: { items: [issue('i1', 'Severance')], nextCursor: null } }
                if (path === '/issues/i1') return { json: { issue: issue('i1', 'Arrival'), youReported: false, reporters: [], events: [{ id: 'e', type: 'reported', actorName: 'Sam', at: '2026-11-01T12:00:00Z' }] } }
                return undefined
            })
            await open(page)
            const results = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze()
            expect(results.violations.map(v => `${v.id}: ${v.nodes.map(n => n.target.join(' ')).join(' | ')}`)).toEqual([])
            expect(await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)).toBeLessThanOrEqual(0)
        })
    }
}
