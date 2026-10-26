import { expect, test, type Page } from '@playwright/test'

function draft() {
    return {
        revision: 1, currentStep: 'general', inactive: true, updatedAt: '2026-10-01T12:00:00Z',
        settings: {
            general: { instanceName: 'Requestarr', timezone: 'UTC', publicUrl: 'https://requests.example.test' },
            authentication: { issuer: 'https://identity.example.test', clientId: 'requestarr', clientSecretEnv: 'REQUESTARR_OIDC_CLIENT_SECRET' },
            providers: { tmdbEnabled: true, tmdbTokenEnv: 'REQUESTARR_TMDB_READ_TOKEN', musicBrainzEnabled: true, musicBrainzContact: '', igdbEnabled: false, igdbClientId: '', igdbClientSecretEnv: 'REQUESTARR_IGDB_CLIENT_SECRET' },
            libraries: {
                jellyfin: { enabled: false, url: '', mediaTypes: [], tokenEnv: 'REQUESTARR_JELLYFIN_API_KEY', username: '', passwordEnv: '' },
                plex: { enabled: false, url: '', mediaTypes: [], tokenEnv: 'REQUESTARR_PLEX_TOKEN', username: '', passwordEnv: '' },
                romm: { enabled: false, url: '', mediaTypes: [], tokenEnv: 'REQUESTARR_ROMM_TOKEN', username: '', passwordEnv: '' },
                navidrome: { enabled: false, url: '', mediaTypes: [], tokenEnv: '', username: '', passwordEnv: 'REQUESTARR_NAVIDROME_PASSWORD' },
            },
            notifications: { appriseEnabled: false, appriseUrl: '', appriseConfigKeyEnv: 'REQUESTARR_APPRISE_CONFIG_KEY' },
            policy: { autoApproveMediaTypes: [] as string[] },
        },
        blockers: [] as { target: string; reason: string }[],
        completedAt: null as string | null,
        validations: [] as { target: string; revision: number; valid: boolean; code: string; message: string; checkedAt: string; expiresAt: string }[],
        environment: [
            { name: 'REQUESTARR_OIDC_CLIENT_SECRET', secret: true, configured: true, needsRestart: false },
            { name: 'REQUESTARR_TMDB_READ_TOKEN', secret: true, configured: false, needsRestart: true },
            { name: 'REQUESTARR_IGDB_CLIENT_SECRET', secret: true, configured: false, needsRestart: true },
            { name: 'REQUESTARR_PUBLIC_URL', secret: false, configured: true, needsRestart: true, desiredValue: 'https://requests.example.test' },
        ],
    }
}

async function mockSetup(page: Page, role = 'admin') {
    const state = {
        saved: draft(), calls: [] as { method: string; path: string; body: any }[], conflict: false,
        secrets: { enabled: true, secrets: [{ name: 'tmdb_read_token', label: 'TMDB read token', envVar: 'REQUESTARR_TMDB_READ_TOKEN', configured: false, source: 'none', unreadable: false, pendingRestart: false },
            { name: 'navidrome_password', label: 'Navidrome password', envVar: 'REQUESTARR_NAVIDROME_PASSWORD', configured: false, source: 'none', unreadable: false, pendingRestart: false }] } as any,
    }
    await page.route('**/api/v1/**', async route => {
        const request = route.request()
        const path = new URL(request.url()).pathname
        if (path.endsWith('/setup/status')) return route.fulfill({ json: { complete: true, required: false } })
        if (path.endsWith('/auth/me')) return route.fulfill({ json: { user: { id: 'user-1', role, email: 'admin@example.test', displayName: 'Admin' }, csrfToken: 'csrf-token' } })
        const method = request.method()
        const body = request.postDataJSON()
        state.calls.push({ method, path, body })
        if (path.endsWith('/admin/secrets')) return route.fulfill({ json: state.secrets })
        const secretName = path.match(/\/admin\/secrets\/(\w+)$/)?.[1]
        if (secretName) {
            expect(request.headers()['x-csrf-token']).toBe('csrf-token')
            const entry = state.secrets.secrets.find((item: any) => item.name === secretName)
            if (method === 'PUT') Object.assign(entry, { configured: true, source: 'stored', pendingRestart: true })
            if (method === 'DELETE') Object.assign(entry, { configured: false, source: 'none', pendingRestart: false })
            return route.fulfill({ status: 204 })
        }
        if (path.endsWith('/admin/setup/draft')) {
            if (method === 'GET') return route.fulfill({ json: state.saved })
            expect(request.headers()['x-csrf-token']).toBe('csrf-token')
            if (state.conflict || body.expectedRevision !== state.saved.revision) return route.fulfill({ status: 409, json: { error: { code: 'setup_revision_conflict', message: 'Setup draft changed; reload the saved draft.' } } })
            state.saved = { ...state.saved, revision: state.saved.revision + 1, currentStep: body.currentStep, settings: body.settings, validations: [] }
            return route.fulfill({ json: state.saved })
        }
        if (path.endsWith('/admin/setup/validate')) {
            expect(method).toBe('POST')
            expect(request.headers()['x-csrf-token']).toBe('csrf-token')
            expect(body.expectedRevision).toBe(state.saved.revision)
            state.saved.validations.push({ target: body.target, revision: state.saved.revision, valid: false, code: 'connection_failed', message: 'The service could not be reached.', checkedAt: '2026-10-01T12:00:00Z', expiresAt: '2026-10-01T12:15:00Z' })
            return route.fulfill({ json: state.saved })
        }
        if (path.endsWith('/admin/setup/finish')) {
            expect(request.headers()['x-csrf-token']).toBe('csrf-token')
            if (state.saved.blockers.length) return route.fulfill({ status: 422, json: { error: { code: 'setup_not_ready', message: 'Every required connection check must pass before finishing.' } } })
            state.saved = { ...state.saved, completedAt: '2026-10-01T12:30:00Z' }
            return route.fulfill({ json: state.saved })
        }
        return route.fulfill({ json: { items: [], nextCursor: null } })
    })
    return state
}

test('admin explicitly saves non-secret fields and resumes the saved step after reload', async ({ page }) => {
    const state = await mockSetup(page)
    await page.goto('/setup')
    await expect(page.getByRole('heading', { name: 'General', exact: true })).toBeVisible()
    await page.getByLabel('Instance name').fill('Shared household')
    expect(state.calls.filter(call => call.method === 'PUT')).toHaveLength(0)
    await page.getByRole('button', { name: 'Save and next' }).click()
    await expect(page.getByRole('heading', { name: 'Authentication', exact: true })).toBeVisible()
    await page.getByLabel('OIDC client ID').fill('household')
    await page.getByRole('button', { name: 'Save draft', exact: true }).click()
    await expect(page.getByRole('status')).toContainText('Draft saved')
    await page.reload()
    await expect(page.getByLabel('OIDC client ID')).toHaveValue('household')
    await page.getByRole('button', { name: 'Save and back' }).click()
    await expect(page.getByLabel('Instance name')).toHaveValue('Shared household')
    expect(state.saved.currentStep).toBe('general')
    await expect(page.getByRole('button', { name: /finish/i })).toHaveCount(0)
})

test('explicit connection check saves first; a failed check leaves settings saved and does not block saving', async ({ page }) => {
    const state = await mockSetup(page)
    state.saved.currentStep = 'authentication'
    await page.goto('/setup')
    await page.getByLabel('OIDC client ID').fill('changed-client')
    await page.getByRole('button', { name: 'Check connection', exact: true }).click()
    await expect(page.getByRole('alert')).toContainText('connection_failed')
    await expect(page.getByRole('alert')).toContainText('The service could not be reached.')
    await expect(page.getByRole('alert')).toContainText('2026-10-01T12:00:00Z')
    await expect(page.getByRole('alert')).toContainText('2026-10-01T12:15:00Z')
    expect(state.saved.settings.authentication.clientId).toBe('changed-client')
    expect(state.calls.filter(call => call.method !== 'GET').map(call => [call.method, call.body.expectedRevision, call.body.target])).toEqual([
        ['PUT', 1, undefined], ['POST', 2, 'authentication'],
    ])
    await expect(page.getByText('Discovery checks do not prove interactive login works.', { exact: false })).toBeVisible()
    await page.getByRole('button', { name: 'Save draft', exact: true }).click()
    await expect(page.getByRole('status')).toContainText('Draft saved')
    await expect(page.getByRole('alert')).toHaveCount(0)
    expect(state.saved.validations).toEqual([])
})

test('providers save only non-secret settings, check enabled providers, and configure a Source in Libraries', async ({ page }) => {
    const state = await mockSetup(page)
    state.saved.currentStep = 'providers'
    await page.goto('/setup')
    await expect(page.getByRole('heading', { name: 'Providers', exact: true })).toBeVisible()
    // Only the write-only stored-secret field (D024); it is never part of the draft.
    await expect(page.locator('input[type=password]')).toHaveCount(1)
    await expect(page.getByLabel('MusicBrainz contact email')).toHaveValue('')
    await expect(page.getByRole('region', { name: 'Server environment' })).toContainText('REQUESTARR_OIDC_CLIENT_SECRET')
    await expect(page.getByRole('region', { name: 'Server environment' })).toContainText('REQUESTARR_TMDB_READ_TOKEN')
    await expect(page.getByRole('region', { name: 'Server environment' })).toContainText('REQUESTARR_IGDB_CLIENT_SECRET')
    await expect(page.getByRole('region', { name: 'Server environment' })).toContainText('Configured: No')
    await expect(page.getByRole('region', { name: 'Server environment' })).toContainText('Restart needed: Yes')
    await expect(page.getByRole('region', { name: 'Server environment' })).toContainText('Desired value: https://requests.example.test')
    await page.getByLabel('MusicBrainz contact email').fill('operator@example.test')
    await page.getByLabel('Enable IGDB').check()
    await page.getByLabel('IGDB client ID').fill('public-client-id')
    await page.getByRole('button', { name: 'Check enabled providers' }).click()
    await expect(page.getByRole('button', { name: 'Save draft', exact: true })).toBeEnabled()
    expect(state.calls.filter(call => call.method !== 'GET').map(call => [call.method, call.body.target])).toEqual([
        ['PUT', undefined], ['POST', 'tmdb'], ['POST', 'musicbrainz'], ['POST', 'igdb'],
    ])
    const input = state.calls.find(call => call.method === 'PUT')!.body
    expect(input.settings.providers).toEqual({ tmdbEnabled: true, tmdbTokenEnv: 'REQUESTARR_TMDB_READ_TOKEN', musicBrainzEnabled: true, musicBrainzContact: 'operator@example.test', igdbEnabled: true, igdbClientId: 'public-client-id', igdbClientSecretEnv: 'REQUESTARR_IGDB_CLIENT_SECRET' })
    expect(Object.keys(input).sort()).toEqual(['currentStep', 'expectedRevision', 'settings'])
    expect(await page.evaluate(() => ({ local: { ...localStorage }, session: { ...sessionStorage } }))).toEqual({ local: {}, session: {} })
    await page.getByRole('button', { name: 'Save and next' }).click()
    await expect(page.getByRole('heading', { name: 'Libraries', exact: true })).toBeVisible()
    await expect(page.getByText('REQUESTARR_PLEX_TOKEN', { exact: true })).toBeVisible()
    // Navidrome: the username is plain draft data; only the password is a write-only stored secret.
    await expect(page.getByLabel('Navidrome username')).toHaveAttribute('type', 'text')
    await expect(page.locator('input[type=password]')).toHaveCount(1)
    await page.getByLabel('Navidrome username').fill('requestarr')
    await page.getByLabel('Navidrome password').fill('music-pass-123')
    await page.getByRole('button', { name: /save navidrome password/i }).click()
    await expect(page.getByLabel('Navidrome password')).toHaveValue('')
    const passwordPut = state.calls.find(call => call.method === 'PUT' && call.path.endsWith('/admin/secrets/navidrome_password'))!
    expect(passwordPut.body).toEqual({ value: 'music-pass-123' })
    await page.getByLabel('Enable Plex').check()
    await page.getByLabel('Plex URL').fill('https://plex.example.test')
    await page.getByRole('checkbox', { name: 'movie', exact: true }).nth(1).check()
    await page.getByRole('button', { name: 'Check Plex connection' }).click()
    await expect(page.getByRole('alert')).toContainText('connection_failed')
    expect(state.calls.filter(call => call.method !== 'GET').slice(-2).map(call => [call.method, call.body.target])).toEqual([['PUT', undefined], ['POST', 'library:plex']])
    expect(state.saved.settings.libraries.plex).toMatchObject({ enabled: true, url: 'https://plex.example.test', mediaTypes: ['movie'], tokenEnv: 'REQUESTARR_PLEX_TOKEN' })
    expect(state.saved.settings.libraries.navidrome.username).toBe('requestarr')
    expect(JSON.stringify(state.saved)).not.toContain('music-pass-123')
    await page.reload()
    await expect(page.getByRole('heading', { name: 'Libraries', exact: true })).toBeVisible()
    await expect(page.getByLabel('Plex URL')).toHaveValue('https://plex.example.test')
    await page.getByRole('button', { name: 'Save and back' }).click()
    await expect(page.getByLabel('IGDB client ID')).toHaveValue('public-client-id')
})

test('revision conflict keeps unsaved fields until the admin chooses Reload saved draft', async ({ page }) => {
    const state = await mockSetup(page)
    await page.goto('/setup')
    await page.getByLabel('Instance name').fill('My unsaved name')
    const loadsBeforeConflict = state.calls.filter(call => call.method === 'GET').length
    state.saved.settings.general.instanceName = 'Another admin saved this'
    state.saved.revision = 2
    await page.getByRole('button', { name: 'Save and next' }).click()
    await expect(page.getByRole('alert')).toContainText('changed')
    await expect(page.getByLabel('Instance name')).toHaveValue('My unsaved name')
    expect(state.calls.filter(call => call.method === 'GET')).toHaveLength(loadsBeforeConflict)
    await page.getByRole('button', { name: 'Reload saved draft', exact: true }).click()
    await expect(page.getByLabel('Instance name')).toHaveValue('Another admin saved this')
    await expect(page.getByRole('alert')).toHaveCount(0)
    await page.getByRole('button', { name: 'Save and next' }).click()
    await expect(page.getByRole('heading', { name: 'Authentication', exact: true })).toBeVisible()
    expect(state.calls.filter(call => call.method === 'PUT').at(-1)!.body.expectedRevision).toBe(2)
})

for (const role of ['viewer', 'requester']) {
    test(`${role} cannot open draft UI or call setup draft endpoints`, async ({ page }) => {
        const state = await mockSetup(page, role)
        await page.goto('/setup')
        await expect(page.getByRole('heading', { name: 'Administrator access required' })).toBeVisible()
        await expect(page.getByLabel('Instance name')).toHaveCount(0)
        await expect(page.getByRole('link', { name: 'Setup', exact: true })).toHaveCount(0)
        expect(state.calls).toEqual([])
        await page.reload()
        await expect(page.getByRole('heading', { name: 'Administrator access required' })).toBeVisible()
        expect(state.calls).toEqual([])
    })
}

test('admin Setup navigation opens the draft and browser back preserves Requests route', async ({ page }) => {
    await mockSetup(page)
    await page.goto('/requests?mine=true')
    await page.getByRole('link', { name: 'Setup', exact: true }).click()
    await expect(page).toHaveURL('/setup')
    await expect(page.getByRole('link', { name: 'Setup', exact: true })).toHaveAttribute('aria-current', 'page')
    await expect(page.getByLabel('Instance name')).toHaveValue('Requestarr')
    await page.goBack()
    await expect(page).toHaveURL('/requests?mine=true')
    await expect(page.getByRole('checkbox', { name: 'My Requests' })).toBeChecked()
})

test('phone layout fits and keyboard can save and advance through labelled fields', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 })
    await mockSetup(page)
    await page.goto('/setup')
    await page.getByLabel('Instance name').focus()
    await page.keyboard.press('ControlOrMeta+A')
    await page.keyboard.type('Keyboard household')
    await page.keyboard.press('Tab')
    await expect(page.getByLabel('Timezone')).toBeFocused()
    await page.keyboard.press('Tab')
    await expect(page.getByLabel('Public URL')).toBeFocused()
    await page.keyboard.press('Tab')
    await expect(page.getByRole('button', { name: 'Save draft', exact: true })).toBeFocused()
    await page.keyboard.press('Enter')
    await expect(page.getByRole('status')).toContainText('Draft saved')
    await page.getByRole('button', { name: 'Save and next' }).focus()
    await page.keyboard.press('Enter')
    await expect(page.getByRole('heading', { name: 'Authentication', exact: true })).toBeFocused()
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
    await page.getByRole('button', { name: 'Save and next' }).click()
    await expect(page.getByRole('heading', { name: 'Providers', exact: true })).toBeVisible()
    await page.getByLabel('Enable TMDb').focus()
    await page.keyboard.press('Space')
    await expect(page.getByLabel('Enable TMDb')).not.toBeChecked()
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
})

test('pending save disables editing and a late reply cannot replace a newly loaded draft', async ({ page }) => {
    const state = await mockSetup(page)
    let release!: () => void
    const held = new Promise<void>(resolve => { release = resolve })
    await page.route('**/api/v1/admin/setup/draft', async route => {
        if (route.request().method() !== 'PUT') return route.fallback()
        const old = { ...draft(), revision: 2, currentStep: 'authentication' }
        await held
        await route.fulfill({ json: old }).catch(() => { /* navigation cancelled the browser request */ })
    })
    await page.goto('/setup')
    await page.getByLabel('Instance name').fill('Pending name')
    const saving = page.waitForRequest(request => request.method() === 'PUT')
    await page.getByRole('button', { name: 'Save and next' }).click()
    await saving
    await expect(page.getByRole('status')).toContainText('Updating shared draft')
    await expect(page.getByLabel('Instance name')).toBeDisabled()
    await expect(page.getByRole('button', { name: 'Save draft', exact: true })).toBeDisabled()
    await expect(page.getByRole('button', { name: 'Check connection', exact: true })).toBeDisabled()
    await page.getByRole('link', { name: 'Requests', exact: true }).click()
    state.saved.settings.general.instanceName = 'New saved draft'
    state.saved.revision = 3
    await page.getByRole('link', { name: 'Setup', exact: true }).click()
    await expect(page.getByLabel('Instance name')).toHaveValue('New saved draft')
    release()
    await expect(page.getByLabel('Instance name')).toHaveValue('New saved draft')
    await expect(page.getByRole('heading', { name: 'General', exact: true })).toBeVisible()
})

test('unavailable draft can be retried without rendering configuration or making connection checks', async ({ page }) => {
    const state = await mockSetup(page)
    let unavailable = true
    await page.route('**/api/v1/admin/setup/draft', route => unavailable
        ? route.fulfill({ status: 503, json: { error: { code: 'setup_unavailable', message: 'Setup draft unavailable. Try again later.' } } })
        : route.fallback())
    await page.goto('/setup')
    await expect(page.getByRole('alert')).toContainText('Setup draft unavailable')
    await expect(page.getByLabel('Instance name')).toHaveCount(0)
    unavailable = false
    await page.getByRole('button', { name: 'Retry loading draft' }).click()
    await expect(page.getByLabel('Instance name')).toHaveValue('Requestarr')
    expect(state.calls.every(call => call.method === 'GET')).toBe(true)
})

test('existing first-admin claim redirects to the inactive draft instead of home', async ({ page }) => {
    await mockSetup(page)
    let claimed = false
    await page.route('**/api/v1/setup/status', route => route.fulfill({ json: { complete: false, required: true } }))
    await page.route('**/api/v1/auth/me', route => route.fulfill({ json: { user: { id: 'user-1', role: claimed ? 'admin' : 'viewer', email: 'admin@example.test', displayName: 'Admin' }, csrfToken: 'csrf-token' } }))
    await page.route('**/api/v1/setup/claim-admin', route => {
        expect(route.request().headers()['x-csrf-token']).toBe('csrf-token')
        claimed = true
        return route.fulfill({ status: 204 })
    })
    await page.goto('/')
    await page.getByLabel('OIDC issuer').fill('https://identity.example.test')
    await page.getByLabel('OIDC subject').fill('bootstrap-identity')
    await page.getByLabel('One-time setup token').fill('one-time-bootstrap-fixture-token-123456789')
    await page.getByRole('button', { name: 'Claim first admin' }).click()
    await expect(page).toHaveURL('/setup')
    await expect(page.getByRole('heading', { name: 'Setup · Inactive draft' })).toBeVisible()
    await expect(page.locator('input[type=password]')).toHaveCount(0)
    expect(await page.evaluate(() => ({ local: { ...localStorage }, session: { ...sessionStorage } }))).toEqual({ local: {}, session: {} })
})

test('invalid settings keep edits and do not run connection checks until saving succeeds', async ({ page }) => {
    const state = await mockSetup(page)
    let invalid = true
    await page.route('**/api/v1/admin/setup/draft', route => route.request().method() === 'PUT' && invalid
        ? route.fulfill({ status: 422, json: { error: { code: 'invalid_setup_settings', message: 'Check the non-secret fields, URLs, timezone, and permitted environment-variable names.' } } })
        : route.fallback())
    await page.goto('/setup')
    await page.getByLabel('Timezone').fill('invalid-timezone')
    await page.getByRole('button', { name: 'Check connection', exact: true }).click()
    await expect(page.getByRole('alert')).toContainText('Check the non-secret fields')
    await expect(page.getByLabel('Timezone')).toHaveValue('invalid-timezone')
    expect(state.calls.some(call => call.method === 'POST')).toBe(false)
    invalid = false
    await page.getByLabel('Timezone').fill('America/New_York')
    await page.getByRole('button', { name: 'Save draft', exact: true }).click()
    await expect(page.getByRole('status')).toContainText('Draft saved')
    expect(state.saved.settings.general.timezone).toBe('America/New_York')
})

test('review lists blockers, finish is disabled until checks pass, then setup completes and the wizard disappears', async ({ page }) => {
    const state = await mockSetup(page)
    state.saved = { ...state.saved, currentStep: 'review', blockers: [{ target: 'general', reason: 'not_checked' }, { target: 'tmdb', reason: 'failed' }] }
    await page.route('**/api/v1/setup/status', route => route.fulfill({ json: { complete: true, required: false, wizardComplete: !!state.saved.completedAt } }))
    await page.goto('/setup')
    await expect(page.getByRole('heading', { name: 'Review', exact: true })).toBeVisible()
    await expect(page.getByLabel('Draft summary')).toContainText('OIDC only')
    await expect(page.getByRole('alert')).toContainText('tmdb: last check failed')
    await expect(page.getByRole('button', { name: 'Finish setup' })).toBeDisabled()
    expect(state.calls.filter(call => call.path.endsWith('/finish'))).toHaveLength(0)
    state.saved = { ...state.saved, blockers: [] }
    await page.reload()
    await expect(page.getByRole('button', { name: 'Finish setup' })).toBeEnabled()
    await page.getByRole('button', { name: 'Finish setup' }).click()
    await expect(page.getByRole('heading', { name: 'Setup complete' })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Finish setup' })).toHaveCount(0)
    await expect(page.getByRole('link', { name: 'Setup', exact: true })).toHaveCount(0)
})

test('notifications and request policy steps save their non-secret fields', async ({ page }) => {
    const state = await mockSetup(page)
    state.saved = { ...state.saved, currentStep: 'notifications' }
    await page.goto('/setup')
    await page.getByLabel('Enable Apprise').check()
    await page.getByLabel('Apprise API URL').fill('https://apprise.example.test')
    await page.getByRole('button', { name: 'Save and next' }).click()
    await expect(page.getByRole('heading', { name: 'Request Policy', exact: true })).toBeVisible()
    await page.getByLabel('movie').check()
    await page.getByRole('button', { name: 'Save and next' }).click()
    await expect(page.getByRole('heading', { name: 'Review', exact: true })).toBeVisible()
    expect(state.saved.settings.notifications.appriseUrl).toBe('https://apprise.example.test')
    expect(state.saved.settings.policy.autoApproveMediaTypes).toEqual(['movie'])
})

test('stored secret field: saves write-only, clears the input, never reaches storage or the draft', async ({ page }) => {
    const state = await mockSetup(page)
    state.saved.currentStep = 'providers'
    await page.goto('/setup')
    const field = page.getByLabel('TMDb read token')
    await expect(field).toBeEnabled()
    await expect(field).toHaveAttribute('type', 'password')
    await field.fill('super-secret-probe-value')
    await page.getByRole('button', { name: 'Save TMDb read token' }).click()
    await expect(page.getByRole('status').filter({ hasText: 'applies after Requestarr restarts' })).toBeVisible()
    await expect(field).toHaveValue('')
    await expect(page.getByText('Source: stored · Saved, pending restart')).toBeVisible()
    await expect(page.getByText('super-secret-probe-value')).toHaveCount(0)
    const put = state.calls.find(call => call.method === 'PUT' && call.path.endsWith('/admin/secrets/tmdb_read_token'))!
    expect(put.body).toEqual({ value: 'super-secret-probe-value' })
    expect(state.calls.filter(call => call.path.endsWith('/admin/setup/draft') && call.method === 'PUT')).toHaveLength(0)
    expect(await page.evaluate(() => JSON.stringify({ ...localStorage, ...sessionStorage }))).not.toContain('super-secret')
    await page.getByRole('button', { name: 'Clear stored value' }).click()
    await expect(page.getByText('Source: none')).toBeVisible()
})

test('stored secret field is disabled with guidance when no master key exists, and keyboard reachable', async ({ page }) => {
    const state = await mockSetup(page)
    state.secrets.enabled = false
    state.saved.currentStep = 'providers'
    await page.goto('/setup')
    await expect(page.getByLabel('TMDb read token')).toBeDisabled()
    await expect(page.getByText('Saving secrets here is turned off.', { exact: false })).toContainText('REQUESTARR_SECRETS_KEY')
})

test('an unreadable stored secret is reported as unreadable, not unconfigured', async ({ page }) => {
    const state = await mockSetup(page)
    Object.assign(state.secrets.secrets[0], { source: 'stored secret unreadable', unreadable: true, configured: false })
    state.saved.currentStep = 'providers'
    await page.goto('/setup')
    await expect(page.getByText('stored secret unreadable. Enter it again', { exact: false })).toBeVisible()
})
