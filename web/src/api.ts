export type User = { id: string; role: string; email: string; displayName: string }
export type Session = { user: User; csrfToken: string }
export type SetupStatus = { complete: boolean; required: boolean }
export type CatalogItem = { ID: string; MediaType: string; Provider: string; Title: string; Year: number; ArtworkURL: string; ArtworkKind: string; ExternalURL: string }
export type Platform = { providerId: string; key: string; name: string }
export type ResolvedItem = { Item: CatalogItem; Platforms: Platform[] }
export type RequestItem = { ID: string; CatalogItemID: string; Title: string; MediaType: string; ArtworkURL: string; ArtworkKind: string; CatalogPlatforms?: Platform[]; Status: string; Version: number; RequestedBy: string; Note: string; availability: 'unknown' | 'available' | 'missing'; availabilityObservedAt?: string | null; Platforms: { Any: boolean; PlatformKeys?: string[] }; CreatedAt: string; subscribed: boolean; subscriberCount: number; subscriptionSource: 'requester' | 'manual' | '' }
export type JellyfinStatus = { configured: boolean; enabled: boolean; lastSyncStartedAt: string | null; lastSyncSucceededAt: string | null; lastSyncError: string | null }
export type AdminUser = { id: string; email: string; displayName: string; role: 'admin' | 'requester' | 'viewer'; createdAt: string }

const base = '/api/v1'
export async function api<T>(path: string, init: RequestInit = {}, csrf?: string): Promise<T> {
    const headers = new Headers(init.headers)
    if (init.body) headers.set('Content-Type', 'application/json')
    if (csrf) headers.set('X-CSRF-Token', csrf)
    const response = await fetch(base + path, { ...init, headers, credentials: 'same-origin' })
    if (!response.ok) {
        throw new Error(await errorMessage(response))
    }
    if (response.status === 204) return undefined as T
    return response.json() as Promise<T>
}

// Errors use {error:{code,message,requestId}}; show the message and keep the
// request ID so a user can quote it when reporting a problem.
async function errorMessage(response: Response): Promise<string> {
    const body = await response.text()
    try {
        const { error } = JSON.parse(body) as { error?: { message?: string; requestId?: string } }
        if (error?.message) return error.requestId ? `${error.message} (request ${error.requestId})` : error.message
    } catch { /* not the JSON envelope */ }
    return body.trim() || `Request failed (${response.status})`
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
