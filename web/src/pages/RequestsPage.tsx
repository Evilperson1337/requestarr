import { useCallback, useEffect, useRef, useState } from 'react'
import { api, ApiError, type RequestItem, type RequestPage, type User } from '../api'
import { followBackLink, followLink, navigate, useLocation } from '../navigation'
import { RequestCover } from '../components/RequestCover'
import { describeSelection } from '../selection'

const statuses = [['', 'All'], ['requested', 'Requested'], ['approved', 'Approved'], ['available', 'Available'], ['denied', 'Denied'], ['cancelled', 'Cancelled']] as const

type FirstPageSnapshot = { userID: string; view: string; savedAt: number; page: RequestPage }

function firstPageSnapshot(userID: string, view: string): FirstPageSnapshot | null {
    const saved = history.state?.requestFirstPage as FirstPageSnapshot | undefined
    return saved?.userID === userID && saved.view === view && Date.now() - saved.savedAt < 15 * 60 * 1000 ? saved : null
}

export function RequestsPage({ user, csrfToken }: { user: User; csrfToken: string }) {
    const view = useLocation()
    const search = view.includes('?') ? view.slice(view.indexOf('?')) : ''
    const params = new URLSearchParams(search)
    const cursor = params.get('cursor')
    const mine = params.get('mine') === 'true'
    const heading = useRef<HTMLHeadingElement>(null)
    const previousCursor = useRef(cursor)
    const focusResults = useRef(false)
    const [searchDraft, setSearchDraft] = useState(params.get('q') || '')
    const [items, setItems] = useState<RequestItem[]>([])
    const [nextCursor, setNextCursor] = useState<string | null>(null)
    const [loading, setLoading] = useState(true)
    const [error, setError] = useState('')
    const [errorCode, setErrorCode] = useState('')
    const [notice, setNotice] = useState('')
    const [actionError, setActionError] = useState('')
    const [busy, setBusy] = useState(false)
    const [restored, setRestored] = useState(false)
    const action = useRef<AbortController | null>(null)
    const listing = useRef<AbortController | null>(null)
    const load = useCallback((signal?: AbortSignal) => {
        listing.current?.abort()
        const controller = new AbortController()
        listing.current = controller
        const cancel = () => controller.abort()
        signal?.addEventListener('abort', cancel, { once: true })
        if (signal?.aborted) cancel()
        setLoading(true); setError(''); setErrorCode('')
        return api<RequestPage>(`/requests${search}`, { signal: controller.signal })
            .then(result => {
                if (controller.signal.aborted) return
                setItems(result.items); setNextCursor(result.nextCursor); setRestored(false)
                // The API's first page has no continuation-to-start cursor.
                // Keep its original membership in this history entry so Back
                // does not splice a new first page into an older traversal.
                if (!cursor) history.replaceState({ ...history.state, requestFirstPage: result.nextCursor ? { userID: user.id, view, savedAt: Date.now(), page: result } : null }, '')
            })
            .catch(e => { if (!controller.signal.aborted) { setError(e instanceof Error ? e.message : 'Could not load requests'); setErrorCode(e instanceof ApiError ? e.code : '') } })
            .finally(() => { signal?.removeEventListener('abort', cancel); if (!controller.signal.aborted) setLoading(false) })
    }, [search, cursor, user.id, view])
    useEffect(() => {
        if (previousCursor.current !== cursor) focusResults.current = true
        previousCursor.current = cursor
        const controller = new AbortController()
        const saved = !cursor && firstPageSnapshot(user.id, view)
        if (saved) {
            setItems(saved.page.items); setNextCursor(saved.page.nextCursor); setLoading(false); setError(''); setErrorCode(''); setRestored(true)
        } else void load(controller.signal)
        setSearchDraft(new URLSearchParams(search).get('q') || '')
        setActionError(''); setNotice('')
        return () => { controller.abort(); listing.current?.abort(); action.current?.abort() }
    }, [load, search, cursor, user.id, view])
    useEffect(() => {
        if (!loading && focusResults.current) { heading.current?.focus(); focusResults.current = false }
    }, [loading])
    function changeFilters(changes: Record<string, string | null>) {
        const query = new URLSearchParams(search)
        query.delete('cursor')
        for (const [key, value] of Object.entries(changes)) {
            if (value) query.set(key, value)
            else query.delete(key)
        }
        const next = query.toString()
        const href = `/requests${next ? '?' + next : ''}`
        if (href === view) void load()
        else navigate(href)
    }
    function pageURL(cursor: string | null) {
        const query = new URLSearchParams(search)
        if (cursor) query.set('cursor', cursor)
        else query.delete('cursor')
        return '/requests' + (query.size ? '?' + query.toString() : '')
    }
    const previous = history.state?.requestPrevious
    const previousPage = typeof previous === 'string' && (previous === '/requests' || previous.startsWith('/requests?')) ? previous : null
    const hasFilters = !!(params.get('q') || params.get('requestId') || params.get('status') || params.get('mediaType') || mine)
    function updateRequest(id: string, patch: Partial<RequestItem>) {
        const update = (items: RequestItem[]) => items.map(item => item.ID === id ? { ...item, ...patch } : item)
        setItems(update)
        const saved = !cursor && firstPageSnapshot(user.id, view)
        if (saved) history.replaceState({ ...history.state, requestFirstPage: { ...saved, page: { ...saved.page, items: update(saved.page.items) } } }, '')
    }
    async function runAction(work: (signal: AbortSignal) => Promise<string | null>) {
        if (action.current) return
        const controller = new AbortController()
        action.current = controller
        setBusy(true); setActionError(''); setNotice('')
        try {
            const message = await work(controller.signal)
            if (controller.signal.aborted || message === null) return
            setNotice(message)
            if (!firstPageSnapshot(user.id, view)) await load(controller.signal)
        } catch (e) {
            if (!controller.signal.aborted) setActionError(e instanceof Error ? e.message : 'Could not update request')
        } finally {
            if (action.current === controller) { action.current = null; setBusy(false) }
        }
    }
    async function transition(req: RequestItem, toStatus: 'approved' | 'denied' | 'cancelled') {
        await runAction(async signal => {
            const result = await api<{ Request: RequestItem }>(`/requests/${req.ID}/transition`, { method: 'POST', signal, body: JSON.stringify({ toStatus, expectedVersion: req.Version, operationId: crypto.randomUUID() }) }, csrfToken)
            if (!signal.aborted) updateRequest(req.ID, { Status: result.Request.Status, Version: result.Request.Version })
            return `The request for ${req.Title} was ${toStatus}.`
        })
    }
    async function changeSubscription(req: RequestItem) {
        await runAction(async signal => {
            const url = `/requests/${req.ID}/subscription`
            const method = req.subscribed ? 'DELETE' : 'PUT'
            let result: { cancelled?: boolean }
            try {
                result = await api(url, { method, signal }, csrfToken)
            } catch (e) {
                // The server decides whether leaving cancels the request, so the warning is never stale.
                if (!(e instanceof ApiError && e.code === 'would_cancel')) throw e
                if (signal.aborted || !confirm(`You are the last subscriber. Leaving cancels the request for ${req.Title}. Continue?`)) return null
                result = await api(`${url}?confirmCancel=true`, { method, signal }, csrfToken)
            }
            if (!signal.aborted) updateRequest(req.ID, { subscribed: !req.subscribed, subscriptionSource: req.subscribed ? '' : 'manual', ...(result.cancelled ? { Status: 'cancelled' } : {}) })
            if (result.cancelled) return `The request for ${req.Title} was cancelled because you were its last subscriber.`
            return req.subscribed ? `You left the request for ${req.Title}.` : `Notifications are on for ${req.Title}.`
        })
    }
    return <main className="shell requestsPage"><section className="queue"><div className="queueHeader"><h1 ref={heading} tabIndex={-1}>Requests</h1><label className="toggle" title="Show only the requests you subscribe to"><input type="checkbox" checked={mine} onChange={e => changeFilters({ mine: e.target.checked ? 'true' : null })} /> My Requests</label></div>
        <div className="queueTools">
            <form className="requestSearch" role="search" onSubmit={event => { event.preventDefault(); changeFilters({ q: searchDraft.trim() }) }}>
                <label htmlFor="request-search" className="sr-only">Search titles</label>
                <div><input id="request-search" type="search" placeholder="Search titles" value={searchDraft} maxLength={200} onChange={event => setSearchDraft(event.target.value)} /><button className="button" type="submit">Search</button></div>
            </form>
            <div className="statusFilters" role="group" aria-label="Request status">{statuses.map(([status, label]) => <button key={status} className="button secondary" aria-pressed={(params.get('status') || '') === status} onClick={() => changeFilters({ status })}>{label}</button>)}</div>
            <div className="requestFilters">
                <label>Media type<select value={params.get('mediaType') || ''} onChange={event => changeFilters({ mediaType: event.target.value })}><option value="">All types</option><option value="movie">Movie</option><option value="tv">TV</option><option value="album">Album</option><option value="song">Song</option><option value="game">Game</option></select></label>
                <label>Sort by<select value={params.get('sort') || 'newest'} onChange={event => changeFilters({ sort: event.target.value === 'newest' ? null : event.target.value })}><option value="newest">Newest first</option><option value="oldest">Oldest first</option><option value="status">Status</option></select></label>
                <label>Requests per page<select value={params.get('limit') || '100'} onChange={event => changeFilters({ limit: event.target.value })}>{params.has('limit') && !['25', '50', '100'].includes(params.get('limit')!) && <option value={params.get('limit')!}>{params.get('limit')}</option>}<option value="25">25</option><option value="50">50</option><option value="100">100</option></select></label>
                {hasFilters && <button className="button secondary" onClick={() => navigate('/requests')}>Clear filters</button>}
            </div>
        </div>
        {actionError && <p role="alert" className="errorBox">{actionError}</p>}
        {restored && <p role="status">Restored the original first page. Details are from its last load; use Refresh view to update the view.</p>}
        {notice && <p role="status">{notice}</p>}{loading && <p role="status" aria-busy="true">Loading requests…</p>}{error && <div role="alert" className="errorBox"><p>{error}</p>{errorCode === 'cursor_expired' || errorCode === 'invalid_cursor' ? <button className="button secondary" onClick={() => changeFilters({})}>Restart from first page</button> : <button className="button secondary" onClick={() => void load()}>Retry</button>}</div>}
        {!loading && !error && items.length === 0 && <div className="empty"><h2>{hasFilters ? 'No matching requests' : 'No requests yet'}</h2><p>{hasFilters ? 'Try a different title, status, media type, or turn off My Requests.' : 'Be the first to request something for the shared library.'}</p></div>}
        {!error && items.length > 0 && <ul className="requestList" aria-label="Requests" aria-busy={loading || busy}>{items.map(req => <li className="requestRow" key={req.ID}>
            <RequestCover req={req} />
            <div className="requestInfo">
                <h2>{req.Title}</h2>
                <p className="requestFacts">{[req.MediaType, `requested ${new Date(req.CreatedAt).toLocaleDateString()}`, req.availability === 'available' ? 'In library' : req.availability === 'missing' ? 'Not in library' : '', req.MediaType === 'game' ? `Platforms: ${req.Platforms?.any ? 'any' : (req.Platforms?.platformKeys || []).map(key => req.CatalogPlatforms?.find(p => p.key === key)?.name || key).join(', ') || 'not specified'}` : ''].filter(Boolean).join(' · ')}</p>
                {describeSelection(req.selection) && <p className="requestSelection"><strong>Requested:</strong> {describeSelection(req.selection)}</p>}
                {req.Note && <p className="requestNote">{req.Note}</p>}
            </div>
            <div className="requestMeta"><span className={`badge status-${req.Status}`}>{req.Status}</span><span className="requester">{req.RequestedBy === user.id ? 'Yours' : 'Shared'}</span></div>
            <div className="requestActions" role="group" aria-label={`Actions for ${req.Title}`}>
                {user.role === 'admin' && (req.Status === 'requested' || req.Status === 'approved') && <>
                    {req.Status === 'requested' && (['approved', 'denied'] as const).map(toStatus => <button key={toStatus} className="button secondary" disabled={busy || loading} onClick={() => void transition(req, toStatus)}>{toStatus === 'approved' ? 'Approve' : 'Deny'}</button>)}
                    <button className="button secondary" disabled={busy || loading} onClick={() => { if (confirm(`Cancel the request for ${req.Title} for everyone?`)) void transition(req, 'cancelled') }}>Cancel request</button>
                </>}
                {(req.subscribed || req.Status === 'requested' || req.Status === 'approved') && <button className="button secondary" disabled={busy || loading} onClick={() => void changeSubscription(req)}>{req.subscribed ? 'Leave request' : 'Notify Me'}</button>}
            </div>
        </li>)}</ul>}
        {!loading && !error && <nav className="requestPaging" aria-label="Request pages">
            {params.has('cursor') && <a className="button secondary" href={pageURL(null)} onClick={followLink}>First page</a>}
            {params.has('cursor') && previousPage && <a className="button secondary" href={previousPage} onClick={followBackLink}>Previous page</a>}
            <p role="status">{items.length} {items.length === 1 ? 'request' : 'requests'} on this page{!nextCursor && items.length > 0 ? ' · End of results' : ''}</p>
            {nextCursor && <a className="button secondary" href={pageURL(nextCursor)} onClick={event => followLink(event, { requestPrevious: view })}>Next page</a>}
        </nav>}
        <p className="paginationHint">Pages keep their original order for up to 15 minutes while details stay current. Refresh the view to include newly matching requests.</p>
        <button className="button secondary" disabled={loading || busy} onClick={() => { setActionError(''); setNotice(''); if (params.has('cursor')) changeFilters({}); else void load() }}>Refresh view</button>
    </section></main>
}
