import { useCallback, useEffect, useRef, useState, type FormEvent } from 'react'
import { api, type MoviePreviewState, type RequestItem, type RequestPage, type ResolvedItem, type Session } from '../api'
import { RequestCover } from '../components/RequestCover'
import { RequestModal } from '../components/RequestModal'
import { followLink } from '../navigation'

export function HomePage({ session, onCreated }: { session: Session; onCreated: () => void }) {
    const [url, setURL] = useState('')
    const [item, setItem] = useState<ResolvedItem | null>(null)
    const [busy, setBusy] = useState(false)
    const [error, setError] = useState('')
    const previewButton = useRef<HTMLButtonElement>(null)
    const returnFocus = useRef<HTMLElement | null>(null)
    const lookup = useRef<AbortController | null>(null)
    const cached = useRef<{ key: string; item: ResolvedItem } | null>(null)
    useEffect(() => () => lookup.current?.abort(), [])
    // The requests this person follows, newest first: the screen's second question after "what do I want".
    const [recent, setRecent] = useState<RequestItem[] | null>(null)
    const [recentFailed, setRecentFailed] = useState(false)
    const loadRecent = useCallback(async (signal?: AbortSignal) => {
        try {
            const page = await api<RequestPage>('/requests?mine=true&limit=5', { cache: 'no-store', signal })
            if (!signal?.aborted) { setRecent(Array.isArray(page?.items) ? page.items : []); setRecentFailed(false) }
        } catch { if (!signal?.aborted) setRecentFailed(true) }
    }, [])
    useEffect(() => { const controller = new AbortController(); void loadRecent(controller.signal); return () => controller.abort() }, [session.user.id, loadRecent])
    async function resolve(e: FormEvent) {
        if (busy) { e.preventDefault(); return }
        returnFocus.current = document.activeElement instanceof HTMLElement ? document.activeElement : previewButton.current
        e.preventDefault(); setBusy(true); setError(''); setItem(null)
        lookup.current?.abort()
        const controller = new AbortController(); lookup.current = controller
        const key = `${session.user.id}:${session.user.role}:${url.trim()}`
        try {
            let result: ResolvedItem
            if (cached.current?.key === key && ['movie', 'tv'].includes(cached.current.item.Item.MediaType)) {
                const state = await api<MoviePreviewState>(`/catalog/${encodeURIComponent(cached.current.item.Item.ID)}/preview`, { signal: controller.signal, cache: 'no-store' })
                result = { ...cached.current.item, Preview: state }
            } else result = await api<ResolvedItem>('/resolve', { method: 'POST', signal: controller.signal, body: JSON.stringify({ url }) }, session.csrfToken)
            if (!controller.signal.aborted) { cached.current = { key, item: result }; setItem(result) }
        }
        catch (e) { if (!controller.signal.aborted) { setError(e instanceof Error ? e.message : 'Could not resolve that link'); cached.current = null } }
        finally { if (!controller.signal.aborted) setBusy(false) }
    }
    return <main className="shell">
        <section className="card hero"><h1>Request Something</h1><p>Paste a TMDB, MusicBrainz, or IGDB link to preview it before requesting.</p>
            <form onSubmit={resolve}><label className="urlBox"><span aria-hidden="true">↗</span><span className="sr-only">TMDB, MusicBrainz, or IGDB URL</span><input type="url" required value={url} onChange={e => setURL(e.target.value)} placeholder="Paste a supported link…" /></label><button ref={previewButton} className="button" disabled={busy} onClick={event => { if (event.detail > 0) event.currentTarget.focus() }}>{busy ? 'Resolving…' : 'Preview'}</button></form>
            {busy && <p role="status">Looking up metadata…</p>}{error && <p role="alert" className="error">{error}</p>}
            <div className="providers"><span>TMDB · Movies &amp; TV</span><span>MusicBrainz · Music</span><span>IGDB · Games</span></div>
        </section>
        <section className="recent" aria-labelledby="recent-heading">
            <div className="recentHeader"><h2 id="recent-heading">Your recent requests</h2><a href="/requests?mine=true" onClick={followLink}>Open the full queue</a></div>
            {recent === null ? <p>{recentFailed ? 'Your recent requests could not be loaded.' : 'Loading…'}</p> : recent.length === 0 ? <p>Nothing yet. Paste a link above to make your first request.</p> : <ul className="recentGrid">{recent.map(req => <li key={req.ID}>
                <a className="recentCard" href={`/requests?requestId=${encodeURIComponent(req.ID)}`} onClick={followLink}>
                    <RequestCover req={req} />
                    <span className="recentTitle">{req.Title}</span>
                    <span className={`badge status-${req.Status}`}>{req.Status}</span>
                </a>
            </li>)}</ul>}
        </section>
        {item && <RequestModal resolved={item} session={session} onCreated={onCreated} onClose={() => { setItem(null); void loadRecent(); if (['movie', 'tv'].includes(item.Item.MediaType)) requestAnimationFrame(() => returnFocus.current?.focus()) }} />}
    </main>
}
