import { useCallback, useEffect, useState } from 'react'
import { api, type RequestItem, type User } from '../api'

declare global { interface Window { requestarrCSRF?: string } }

export function RequestsPage({ user, csrfToken }: { user: User; csrfToken: string }) {
    const [mine, setMine] = useState(() => new URLSearchParams(location.search).get('mine') === 'true')
    const [items, setItems] = useState<RequestItem[]>([])
    const [loading, setLoading] = useState(true)
    const [error, setError] = useState('')
    const [notice, setNotice] = useState('')
    const load = useCallback(() => {
        setLoading(true); setError('')
        api<{ items: RequestItem[] }>(`/requests${mine ? '?mine=true' : ''}`).then(result => setItems(result.items || []))
            .catch(e => setError(e instanceof Error ? e.message : 'Could not load requests')).finally(() => setLoading(false))
    }, [mine])
    useEffect(() => { load() }, [load])
    useEffect(() => {
        const sync = () => setMine(new URLSearchParams(location.search).get('mine') === 'true')
        window.addEventListener('popstate', sync)
        return () => window.removeEventListener('popstate', sync)
    }, [])
    function changeMine(value: boolean) {
        setMine(value)
        const url = new URL(location.href)
        if (value) url.searchParams.set('mine', 'true')
        else url.searchParams.delete('mine')
        history.pushState({}, '', url)
    }
    async function transition(req: RequestItem, toStatus: 'approved' | 'denied' | 'cancelled') {
        try { await api(`/requests/${req.ID}/transition`, { method: 'POST', body: JSON.stringify({ toStatus, expectedVersion: req.Version, operationId: crypto.randomUUID() }) }, csrfToken); load() } catch (e) { setError(e instanceof Error ? e.message : 'Could not transition request'); load() }
    }
    async function changeSubscription(req: RequestItem) {
        setNotice('')
        if (req.subscribed && req.subscriberCount <= 1 && !confirm(`You are the last subscriber. Leaving cancels the request for ${req.Title}. Continue?`)) return
        try {
            const result = await api<{ cancelled?: boolean }>(`/requests/${req.ID}/subscription`, { method: req.subscribed ? 'DELETE' : 'PUT' }, csrfToken)
            if (result?.cancelled) setNotice(`The request for ${req.Title} was cancelled because you were its last subscriber.`)
            load()
        } catch (e) { setError(e instanceof Error ? e.message : 'Could not update subscription') }
    }
    return <main className="shell"><section className="card"><div className="pageHeading"><div><p className="eyebrow">Availability tracking</p><h1>Requests</h1><p>One shared queue for everyone. Filter to requests you follow or created.</p></div><label className="toggle"><input type="checkbox" checked={mine} onChange={e => changeMine(e.target.checked)} /> My Requests</label></div>
        {notice && <p role="status">{notice}</p>}{loading && <p role="status" aria-busy="true">Loading requests…</p>}{error && <div role="alert" className="errorBox"><p>{error}</p><button className="button secondary" onClick={load}>Retry</button></div>}
        {!loading && !error && items.length === 0 && <div className="empty"><h2>{mine ? 'No requests you follow yet' : 'No requests yet'}</h2><p>{mine ? 'Requests you create or choose to follow with Notify Me will appear here.' : 'Be the first to request something for the shared library.'}</p></div>}
        {!loading && !error && items.length > 0 && <ul className="requestList">{items.map(req => <li className="requestRow" key={req.ID}>{req.ArtworkURL && <img src={req.ArtworkURL} alt={`Artwork for ${req.Title}`} className={`art small${req.ArtworkKind === 'square' ? ' square' : ''}`} />}<div className="requestInfo"><h2>{req.Title}</h2><p>{req.MediaType} · requested {new Date(req.CreatedAt).toLocaleDateString()}</p><p>Availability: <strong>{req.availability === 'available' ? 'Available in library' : req.availability === 'missing' ? 'Not currently observed' : 'Unknown'}</strong>{req.availabilityObservedAt && <> · last observed {new Date(req.availabilityObservedAt).toLocaleString()}</>}</p>{req.Note && <p>{req.Note}</p>}<p>Platforms: {req.Platforms?.Any ? 'Any' : (req.Platforms?.PlatformKeys || []).map(key => req.CatalogPlatforms?.find(p => p.key === key)?.name || key).join(', ') || 'Not specified'}</p></div><span className={`badge status-${req.Status}`}>{req.Status}</span><span className="requester">{req.RequestedBy === user.id ? 'You' : 'Shared request'}</span>{user.role === 'admin' && (req.Status === 'requested' || req.Status === 'approved') && <div className="transitionControls">{req.Status === 'requested' && (['approved', 'denied'] as const).map(toStatus => <button key={toStatus} className="button secondary" onClick={() => transition(req, toStatus)}>{toStatus === 'approved' ? 'Approve' : 'Deny'}</button>)}<button className="button secondary" onClick={() => { if (confirm(`Cancel the request for ${req.Title} for everyone?`)) transition(req, 'cancelled') }}>Cancel request</button></div>}{(req.Status === 'requested' || req.Status === 'approved') && <button className="button secondary" onClick={() => changeSubscription(req)}>{req.subscribed ? 'Leave request' : 'Notify Me'}</button>}</li>)}</ul>}
    </section></main>
}
