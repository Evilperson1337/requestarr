export type User = { id: string; role: string; email: string; displayName: string }
export type Session = { user: User; csrfToken: string }
export type SetupStatus = { complete: boolean; required: boolean; wizardComplete?: boolean }
export type SetupStep = 'general' | 'authentication' | 'providers' | 'libraries' | 'notifications' | 'policy' | 'review'
export type SetupTarget = 'general' | 'authentication' | 'tmdb' | 'musicbrainz' | 'igdb' | 'library:jellyfin' | 'library:plex' | 'library:romm' | 'library:navidrome' | 'notifications'
export type SourceKind = { slug: string; name: string; urlEnv: string; mediaTypes: SetupSource['mediaTypes']; credentials: { role: 'token' | 'username' | 'password'; env: string; secret?: string; label?: string }[] }
export type SetupSource = { enabled: boolean; url: string; mediaTypes: ('movie' | 'tv' | 'album' | 'song' | 'game')[]; tokenEnv: string; username: string; passwordEnv: string }
export type SetupSettings = {
    general: { instanceName: string; timezone: string; publicUrl: string }
    authentication: { issuer: string; clientId: string; clientSecretEnv: string }
    providers: { tmdbEnabled: boolean; tmdbTokenEnv: string; musicBrainzEnabled: boolean; musicBrainzContact: string; igdbEnabled: boolean; igdbClientId: string; igdbClientSecretEnv: string }
    libraries: { jellyfin: SetupSource; plex: SetupSource; romm: SetupSource; navidrome: SetupSource }
    notifications: { appriseEnabled: boolean; appriseUrl: string; appriseConfigKeyEnv: string }
    policy: { autoApproveMediaTypes: SetupSource['mediaTypes'] }
}
export type SetupDraft = {
    revision: number; currentStep: SetupStep; settings: SetupSettings; updatedAt: string; inactive: boolean
    validations: { target: SetupTarget; revision: number; valid: boolean; code: string; message: string; checkedAt: string; expiresAt: string }[] | null
    environment: { name: string; secret: boolean; configured: boolean; needsRestart: boolean; desiredValue?: string }[] | null
    blockers: { target: SetupTarget; reason: 'not_checked' | 'failed' | 'expired' }[] | null
    completedAt: string | null
}
export type CatalogItem = { ID: string; MediaType: string; Provider: string; Title: string; Year: number; ArtworkURL: string; ArtworkKind: string; ExternalURL: string }
export type Selection = { tracks?: { id: string; position?: number; title: string }[]; seasons?: number[]; episodes?: { season: number; episode: number }[] }
export type Platform = { providerId: string; key: string; name: string }
export type MovieDetails = { overview?: string; runtime?: number; genres?: string[]; releaseDate?: string; releaseStatus?: string; score?: number; ratingCount?: number; backdropUrl?: string; directors?: string[]; cast?: { name: string; character?: string; profileUrl?: string }[]; detailsUnavailable?: boolean }
export type TVDetails = Omit<MovieDetails, 'runtime' | 'releaseDate' | 'releaseStatus' | 'directors'> & { episodeRuntime?: number; firstAirDate?: string; lastAirDate?: string; seriesStatus?: string; seasons?: number; episodes?: number; creators?: string[]; seasonList?: { number: number; name?: string; episodeCount?: number; airDate?: string; episodes?: { number: number; name?: string; airDate?: string }[] }[] }
export type GameDetails = { overview?: string; genres?: string[]; releaseDate?: string; developers?: string[]; score?: number; ratingCount?: number; backdropUrl?: string }
export type MusicDetails = { artists?: string[]; primaryType?: string; secondaryTypes?: string[]; releaseDate?: string; genres?: string[]; lengthSeconds?: number; appearsOn?: string[]; tracks?: { id: string; position: number; title: string; lengthSeconds?: number }[]; score?: number; ratingCount?: number }
export type MoviePreviewState = { availability: 'unknown' | 'available' | 'missing'; canRequest: boolean; canSubscribe: boolean; canExtend?: boolean; request?: { id: string; status: string; subscribed: boolean; selection?: Selection }; libraries?: { name: string; url?: string }[] }
export type ResolvedItem = { Item: CatalogItem; Platforms: Platform[]; Movie?: MovieDetails; TV?: TVDetails; Game?: GameDetails; Music?: MusicDetails; Preview?: MoviePreviewState }
export type RequestItem = { ID: string; CatalogItemID: string; Title: string; MediaType: string; ArtworkURL: string; ArtworkKind: string; CatalogPlatforms?: Platform[]; Status: string; Version: number; RequestedBy: string; Note: string; availability: 'unknown' | 'available' | 'missing'; availabilityObservedAt?: string | null; Platforms: { any: boolean; platformKeys?: string[] }; CreatedAt: string; subscribed: boolean; subscriptionSource: 'requester' | 'manual' | ''; selection?: Selection }
export type RecentLibraryItem = { id: string; title: string; year?: number; mediaType: 'movie' | 'tv' | 'album' | 'game'; detail?: string; source: string; addedAt: string; artworkUrl?: string }
export type IssueKindOption = { kind: string; label: string }
export type LibraryItem = { id: string; title: string; year?: number; mediaType: 'movie' | 'tv' | 'album' | 'game'; source: string; sourceKind: string; addedAt: string; detail?: string; artworkUrl?: string; link?: string; openIssues: number; issueKinds: IssueKindOption[]; tracks?: { id: string; title: string; disc?: number; position?: number }[] }
export type LibraryPage = { items: LibraryItem[]; nextCursor: string | null }
export type IssueStatus = 'open' | 'resolved' | 'closed'
export type Issue = { id: string; libraryItemId: string; title: string; mediaType: string; source: string; kind: string; kindLabel: string; part: Selection; status: IssueStatus; description?: string; reporters: number; goneFromLibrary: boolean; createdAt: string; updatedAt: string; resolvedAt?: string }
export type IssueDetail = { issue: Issue; events: { id: string; type: string; actorName: string; comment?: string; at: string }[]; reporters: { userId: string; name: string; note?: string; at: string; left: boolean }[]; youReported: boolean }
export type RequestPage = { items: RequestItem[]; nextCursor: string | null }
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
    constructor(message: string, readonly code = '', readonly status = 0) { super(message) }
}

async function apiError(response: Response): Promise<ApiError> {
    const body = await response.text()
    try {
        const { error } = JSON.parse(body) as { error?: { code?: string; message?: string; requestId?: string } }
        if (error?.message) return new ApiError(error.requestId ? `${error.message} (request ${error.requestId})` : error.message, error.code, response.status)
    } catch { /* not the JSON envelope */ }
    return new ApiError(body.trim() || `Request failed (${response.status})`, '', response.status)
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

export type SecretStatus = {
    name: string; label: string; envVar: string; configured: boolean
    source: 'none' | 'environment' | 'stored' | 'stored (shadowed by environment)' | 'stored secret unreadable'
    unreadable: boolean; pendingRestart: boolean
}
export type LibraryConnection = { slug: string; name: string; url: string; urlFromEnvironment: boolean; urlEnv: string; saved: boolean; takesUsername: boolean; username: string; active: boolean; missing: string[]; credentials: { secret: string; label: string; configured: boolean; source: string; unreadable: boolean }[] }
export type ConnectionTest = { ok: boolean; code: string; message: string }
export type SecretsOverview = { enabled: boolean; secrets: SecretStatus[] }

export type JobRun = { id: string; sourceId: string; trigger: 'schedule' | 'manual'; status: 'running' | 'succeeded' | 'failed'; startedAt: string; finishedAt: string | null; error?: string }
export type OperationalStatus = {
    developmentLogsEnabled?: true
    providers: { key: string; name: string; configured: boolean; note?: string }[]
    sources: { slug: string; name: string; configured: boolean; lastRun: JobRun | null; lastSuccessAt: string | null; lastSnapshotAt: string | null; lastError: string; note?: string }[]
    recentErrors: { source: string; run: JobRun }[]
}
export type SourceRuns = { sourceId: string; name: string; runs: JobRun[] }

export type DevelopmentLogEntry = {
    seq: number; time: string; target: SetupTarget
    stage: 'check' | 'request' | 'oauth' | 'platforms'
    outcome: 'success' | 'failure' | 'skipped'
    code: 'connected' | 'connection_failed' | 'authentication_failed' | 'environment_required' | 'incomplete_settings' | 'disabled' | 'http_status' | 'network' | 'network_timeout' | 'network_dns' | 'content_type' | 'response_size' | 'malformed_response'
    httpStatus?: number; durationMs: number
}
export type DevelopmentLogs = { entries: DevelopmentLogEntry[]; capacity: number }
