export type User = { id: string; role: string; email: string; displayName: string }
export type Session = { user: User; csrfToken: string }
export type SetupStatus = { complete: boolean; required: boolean }
export type CatalogItem = { ID: string; MediaType: string; Provider: string; Title: string; Year: number; ArtworkURL: string; ArtworkKind: string; ExternalURL: string }
export type Platform = { providerId: string; key: string; name: string }
export type ResolvedItem = { Item: CatalogItem; Platforms: Platform[] }
export type RequestItem = { ID: string; CatalogItemID: string; Title: string; MediaType: string; ArtworkURL: string; ArtworkKind: string; CatalogPlatforms?: Platform[]; Status: string; Version: number; RequestedBy: string; Note: string; availability: 'unknown' | 'available' | 'missing'; availabilityObservedAt?: string | null; Platforms: { any: boolean; platformKeys?: string[] }; CreatedAt: string; subscribed: boolean; subscriptionSource: 'requester' | 'manual' | '' }
export type RequestPage = { items: RequestItem[]; nextCursor: string | null }
export type JellyfinStatus = { configured: boolean; enabled: boolean; lastSyncStartedAt: string | null; lastSyncSucceededAt: string | null; lastSyncError: string | null }
export type AdminUser = { id: string; email: string; displayName: string; role: 'admin' | 'requester' | 'viewer'; createdAt: string }
export type MatchSuggestion = { id: string; catalogItemId: string; catalogTitle: string; catalogYear: number; inventoryTitle: string; inventoryYear: number; mediaType: string; platformKey: string; sourceName: string; sourceItemId: string; confidence: 'title_year' | 'title_only'; reason: string; observedAt: string }

const base = '/api/v1'
export async function api<T>(path: string, init: RequestInit = {}, csrf?: string): Promise<T> {
    const headers = new Headers(init.headers)
    if (init.body) headers.set('Content-Type', 'application/json')
    if (csrf) headers.set('X-CSRF-Token', csrf)
    const response = await fetch(base + path, { ...init, headers, credentials: 'same-origin' })
    if (!response.ok) {
        throw await apiError(response)
    }
    if (response.status === 204) return undefined as T
    return response.json() as Promise<T>
}

// Errors use {error:{code,message,requestId}}; keep the code for branching and
// show the message plus request ID so a user can quote it when reporting a problem.
export class ApiError extends Error {
    constructor(message: string, readonly code = '') { super(message) }
}

async function apiError(response: Response): Promise<ApiError> {
    const body = await response.text()
    try {
        const { error } = JSON.parse(body) as { error?: { code?: string; message?: string; requestId?: string } }
        if (error?.message) return new ApiError(error.requestId ? `${error.message} (request ${error.requestId})` : error.message, error.code)
    } catch { /* not the JSON envelope */ }
    return new ApiError(body.trim() || `Request failed (${response.status})`)
}

// Never surface server-provided error bodies from the bootstrap claim endpoint.
export async function claimSetupAdmin(input: { issuer: string; subject: string; token: string }, csrf: string): Promise<void> {
    const response = await fetch(base + '/setup/claim-admin', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
        credentials: 'same-origin',
        cache: 'no-store',
        body: JSON.stringify(input),
    })
    if (!response.ok) throw new Error('Admin claim could not be completed. Check the values and setup token, then try again.')
}

export type JobRun = { id: string; sourceId: string; trigger: 'schedule' | 'manual'; status: 'running' | 'succeeded' | 'failed'; startedAt: string; finishedAt: string | null; error?: string }
export type SourceRuns = { sourceId: string; name: string; runs: JobRun[] }
