import { expect, test, type Page } from '@playwright/test'
import AxeBuilder from '@axe-core/playwright'
import { capabilitiesFor } from './capabilities'

const ID = '66666666-6666-4666-8666-666666666666'
const book = {
    Item: { ID, MediaType: 'audiobook', Provider: 'audible', ProviderID: 'B08G9PRS1K', Title: 'Project Hail Mary', Year: 2021, ArtworkURL: '', ArtworkKind: 'square', ExternalURL: 'https://www.audible.com/pd/B08G9PRS1K',
        Metadata: { asin: 'B08G9PRS1K', region: 'us', authors: ['Andy Weir'], narrators: ['Ray Porter'], abridged: false, runtimeMinutes: 970, series: 'Hail Mary', seriesPosition: '1', subtitle: 'A Novel' } },
    Platforms: null, Preview: { availability: 'unknown', canRequest: true, canSubscribe: false },
}
const placeholder = { ...book, Item: { ...book.Item, Title: 'Audiobook B08G9PRS1K', Year: 0, Metadata: { asin: 'B08G9PRS1K', region: 'us', unresolved: true } } }

async function open(page: Page, resolved: any, input: string, onResolve?: (body: unknown) => void, onRequest?: (body: unknown) => void) {
    await page.route('**/api/v1/**', route => {
        const path = new URL(route.request().url()).pathname
        if (path.endsWith('/auth/me')) return route.fulfill({ json: { user: { id: 'user-1', role: 'requester', displayName: 'User' }, capabilities: capabilitiesFor('requester'), csrfToken: 'csrf-token' } })
        if (path.endsWith('/resolve')) { onResolve?.(route.request().postDataJSON()); return route.fulfill({ json: resolved }) }
        if (path.endsWith('/requests') && route.request().method() === 'POST') {
            onRequest?.(route.request().postDataJSON())
            return route.fulfill({ status: 201, json: { Created: true, Request: { ID: '33333333-3333-4333-8333-333333333333', Status: 'requested' } } })
        }
        if (path.includes('/preview')) return route.fulfill({ json: { availability: 'unknown', canRequest: false, canSubscribe: false, request: { id: '33333333-3333-4333-8333-333333333333', status: 'requested', subscribed: true } } })
        return route.fulfill({ json: { items: [], nextCursor: null } })
    })
    await page.goto('/')
    await page.getByPlaceholder('Search or paste a link…').fill(input)
    await page.getByRole('button', { name: 'Preview', exact: true }).click()
    return page.getByRole('dialog', { name: /Audiobook|Project Hail Mary/ })
}

test('a bare ASIN is accepted and previewed with author, narrator, edition and length', async ({ page }) => {
    let sent: unknown
    const dialog = await open(page, book, 'B08G9PRS1K', body => { sent = body })
    expect(sent).toEqual({ url: 'B08G9PRS1K' })
    await expect(dialog).toContainText('Audiobook preview')
    await expect(dialog).toContainText('Author: Andy Weir')
    await expect(dialog).toContainText('Narrated by: Ray Porter')
    await expect(dialog).toContainText('Length: 16 h 10 min')
    await expect(dialog.getByRole('list', { name: 'Edition' }).getByRole('listitem')).toHaveText(['Unabridged', 'Hail Mary #1', 'Audible US'])
    await expect(dialog.getByRole('link', { name: 'Audible (opens in a new tab)' })).toHaveAttribute('href', book.Item.ExternalURL)
})

test('an unresolved ASIN says its details will fill in and can still be requested', async ({ page }) => {
    let sent: unknown
    const dialog = await open(page, placeholder, 'B08G9PRS1K', undefined, body => { sent = body })
    await expect(dialog).toContainText('Details will fill in')
    await expect(dialog).toContainText('B08G9PRS1K')
    await dialog.getByRole('button', { name: 'Create request' }).click()
    await expect(dialog.getByText('Audiobook requested')).toBeVisible()
    expect(sent).toEqual({ catalogId: ID, note: '', platforms: { any: true } })
})

test('an Audible link with tracking parameters is sent as typed', async ({ page }) => {
    let sent: unknown
    const link = 'https://www.audible.com/pd/Project-Hail-Mary-Audiobook/B08G9PRS1K?qid=1'
    await open(page, book, link, body => { sent = body })
    expect(sent).toEqual({ url: link })
})

test('an audiobook that a library holds says so and offers no new request', async ({ page }) => {
    const held = { ...book, Preview: { availability: 'available', canRequest: false, canSubscribe: false, libraries: [{ name: 'Audiobookshelf', link: 'https://abs.example/item/li_1' }] } }
    const dialog = await open(page, held, 'B08G9PRS1K')
    await expect(dialog.getByRole('button', { name: 'Create request' })).toHaveCount(0)
})

test('audiobook preview has no accessibility violations', async ({ page }) => {
    const dialog = await open(page, book, 'B08G9PRS1K')
    await expect(dialog).toBeVisible()
    expect((await new AxeBuilder({ page }).include('[role="dialog"]').analyze()).violations).toEqual([])
})

test('the Library page has an Audiobooks tab with square covers and no issue button', async ({ page }) => {
    const asked: string[] = []
    await page.route('**/api/v1/**', route => {
        const url = new URL(route.request().url())
        if (url.pathname.endsWith('/auth/me')) return route.fulfill({ json: { user: { id: 'u', role: 'requester', displayName: 'U' }, capabilities: capabilitiesFor('requester'), csrfToken: 'c' } })
        if (url.pathname.endsWith('/library/items')) {
            asked.push(url.searchParams.get('type') ?? '')
            return route.fulfill({ json: { items: url.searchParams.get('type') === 'audiobook' ? [{ id: 'i1', title: 'Project Hail Mary', mediaType: 'audiobook', source: 'Audiobookshelf', sourceKind: 'audiobookshelf', addedAt: '2026-01-01T00:00:00Z', detail: 'Andy Weir · read by Ray Porter', openIssues: 0, issueKinds: [] }] : [], nextCursor: null } })
        }
        return route.fulfill({ json: { items: [], nextCursor: null } })
    })
    await page.goto('/library')
    await page.getByRole('button', { name: 'Audiobooks' }).click()
    await expect(page.getByText('Project Hail Mary')).toBeVisible()
    expect(asked).toContain('audiobook')
    await expect(page.locator('.railCover.square')).toHaveCount(1)
    await page.getByRole('button', { name: /Project Hail Mary/ }).click()
    await expect(page.getByRole('button', { name: 'Report an issue' })).toHaveCount(0)
})
