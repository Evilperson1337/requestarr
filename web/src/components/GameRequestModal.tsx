import { useEffect, useRef, useState, type KeyboardEvent } from 'react'
import { api, ApiError, type MoviePreviewState, type ResolvedItem, type Session } from '../api'
import { followLink } from '../navigation'

// The game counterpart of MovieRequestModal: the same layout and the same
// request/availability state, plus the platform choice a game request needs.
export function GameRequestModal({ resolved, session, onClose }: { resolved: ResolvedItem; session: Session; onCreated: () => void; onClose: () => void }) {
    const item = resolved.Item, game = resolved.Game
    const dialog = useRef<HTMLElement>(null)
    const [expanded, setExpanded] = useState(false)
    const [posterFailed, setPosterFailed] = useState(false)
    const [backdropFailed, setBackdropFailed] = useState(false)
    const [note, setNote] = useState('')
    const [noteOpen, setNoteOpen] = useState(false)
    const [any, setAny] = useState(true)
    const [platforms, setPlatforms] = useState<string[]>([])
    const [preview, setPreview] = useState<MoviePreviewState>(resolved.Preview || { availability: 'unknown', canRequest: session.user.role !== 'viewer', canSubscribe: false })
    const [success, setSuccess] = useState('')
    const [authLost, setAuthLost] = useState(false)
    const pending = useRef<AbortController | null>(null)
    const successMessage = useRef<HTMLParagraphElement>(null)
    const [busy, setBusy] = useState(false)
    const [error, setError] = useState('')
    useEffect(() => {
        const opener = document.activeElement as HTMLElement | null
        const overflow = document.body.style.overflow
        document.body.style.overflow = 'hidden'
        const resize = () => {
            if (!dialog.current) return
            if (window.matchMedia('(max-width: 600px)').matches) dialog.current.style.height = `${window.visualViewport?.height || window.innerHeight}px`
            else dialog.current.style.removeProperty('height')
        }
        resize(); window.visualViewport?.addEventListener('resize', resize); window.addEventListener('resize', resize)
        dialog.current?.focus()
        return () => { pending.current?.abort(); window.visualViewport?.removeEventListener('resize', resize); window.removeEventListener('resize', resize); document.body.style.overflow = overflow; opener?.focus() }
    }, [])
    useEffect(() => { if (success) successMessage.current?.focus() }, [success])
    function keyboard(event: KeyboardEvent) {
        if (event.key === 'Escape') { event.stopPropagation(); onClose(); return }
        if (event.key !== 'Tab' || !dialog.current) return
        const controls = [...dialog.current.querySelectorAll<HTMLElement>('button:not(:disabled), input:not(:disabled), textarea:not(:disabled), a[href]')]
        const first = controls[0], last = controls[controls.length - 1]
        if (event.shiftKey && (document.activeElement === first || document.activeElement === dialog.current)) { event.preventDefault(); last?.focus() }
        else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus() }
    }
    function toggle(key: string) { setAny(false); setPlatforms(current => current.includes(key) ? current.filter(x => x !== key) : [...current, key]) }
    const platformsValid = any || platforms.length > 0
    const canCreate = !authLost && !success && preview.canRequest && !preview.request && preview.availability !== 'available' && session.user.role !== 'viewer'
    async function create() {
        if (pending.current || !canCreate || !platformsValid) return
        const controller = new AbortController(); pending.current = controller
        setBusy(true); setError('')
        try {
            const result = await api<{ Created: boolean; Request: { ID: string; Status: string } }>('/requests', { method: 'POST', signal: controller.signal, body: JSON.stringify({ catalogId: item.ID, note, platforms: { any, platformKeys: any ? [] : platforms } }) }, session.csrfToken)
            if (controller.signal.aborted) return
            if (!result?.Request?.ID) throw new Error('The request result could not be confirmed. Retry safely to check the shared Request.')
            setPreview({ availability: preview.availability, canRequest: false, canSubscribe: !result.Created && ['requested', 'approved'].includes(result.Request.Status), request: { id: result.Request.ID, status: result.Request.Status, subscribed: result.Created } })
            setSuccess(result.Created ? 'Game requested' : 'This game was already requested')
            try { setPreview(await api<MoviePreviewState>(`/catalog/${encodeURIComponent(item.ID)}/preview`, { signal: controller.signal, cache: 'no-store' })) }
            catch (e) { if (!controller.signal.aborted) actionError(e, 'The Request was saved, but its latest state could not be refreshed. Close and preview it again to refresh.') }
        } catch (e) { if (!controller.signal.aborted) actionError(e, 'Could not create the request') }
        finally { if (!controller.signal.aborted) { pending.current = null; setBusy(false) } }
    }
    function actionError(e: unknown, fallback: string) {
        if (e instanceof ApiError && (e.status === 401 || e.status === 403)) {
            setAuthLost(true); setSuccess(''); setNote(''); setPreview({ availability: 'unknown', canRequest: false, canSubscribe: false })
        }
        setError(e instanceof Error ? e.message : fallback)
    }
    async function subscribe() {
        if (pending.current || !preview.request || !preview.canSubscribe || authLost) return
        const controller = new AbortController(); pending.current = controller; setBusy(true); setError('')
        try {
            await api(`/requests/${encodeURIComponent(preview.request.id)}/subscription`, { method: 'PUT', signal: controller.signal }, session.csrfToken)
            if (!controller.signal.aborted) setPreview(current => ({ ...current, canSubscribe: false, request: current.request && { ...current.request, subscribed: true } }))
        } catch (e) { if (!controller.signal.aborted) actionError(e, 'Could not enable notifications') }
        finally { if (!controller.signal.aborted) { pending.current = null; setBusy(false) } }
    }
    const overview = game?.overview || ''
    const longOverview = overview.length > 320
    // Parse date-only values at local noon so locale formatting never shifts a day.
    const releaseDate = game?.releaseDate && /^\d{4}-\d{2}-\d{2}$/.test(game.releaseDate) ? new Date(`${game.releaseDate}T12:00:00`) : null
    const todayISO = new Date().toLocaleDateString('en-CA')
    const upcoming = !!game?.releaseDate && game.releaseDate > todayISO
    return <div className="overlay movieOverlay" onMouseDown={event => { if (event.target === event.currentTarget) onClose() }}>
        <section ref={dialog} tabIndex={-1} onKeyDown={keyboard} role="dialog" aria-modal="true" aria-labelledby="game-preview-title" className="movieModal">
            <div className="movieScroll">
                {game?.backdropUrl && !backdropFailed && <img className="movieBackdrop" src={game.backdropUrl} alt="" onError={() => setBackdropFailed(true)} />}
                <header className="movieHeader"><p className="eyebrow">Game preview</p><button className="close" aria-label="Close preview" onClick={onClose}>×</button></header>
                <div className="movieIdentity">
                    {item.ArtworkURL && !posterFailed ? <img className="moviePoster" src={item.ArtworkURL} alt={`Cover for ${item.Title}`} onError={() => setPosterFailed(true)} /> : <div className="moviePoster moviePlaceholder" role="img" aria-label="Cover unavailable">Cover unavailable</div>}
                    <div className="movieHeading"><h2 id="game-preview-title">{item.Title}{item.Year ? ` (${item.Year})` : ''}</h2>
                        <div className="movieFacts">{upcoming && <span className="movieBadge">Upcoming</span>}{game?.genres?.map((genre, index) => <span key={`${genre}-${index}`}>{genre}</span>)}</div>
                        <p>{releaseDate && !Number.isNaN(releaseDate.getTime()) ? <><strong>Release date:</strong> <time dateTime={game?.releaseDate}>{releaseDate.toLocaleDateString(undefined, { year: 'numeric', month: 'long', day: 'numeric' })}</time></> : 'Release date unknown'}</p>
                        <p className="movieRating">{game?.ratingCount && game.score !== undefined ? <>IGDB <strong>{Math.round(game.score)}/100</strong> · {game.ratingCount.toLocaleString()} ratings</> : 'IGDB · Not yet rated'}</p>
                        {item.ExternalURL && <a href={item.ExternalURL} target="_blank" rel="noreferrer">Open on IGDB ↗</a>}
                    </div>
                </div>
                {overview && <section aria-label="Synopsis"><h3>Synopsis</h3><p id="game-synopsis">{longOverview && !expanded ? `${overview.slice(0, 320)}…` : overview}</p>{longOverview && <button className="button secondary" aria-expanded={expanded} aria-controls="game-synopsis" onClick={() => setExpanded(!expanded)}>{expanded ? 'Show less' : 'Show more'}</button>}</section>}
                {!!game?.developers?.length && <p><strong>Developer{game.developers.length > 1 ? 's' : ''}:</strong> {game.developers.join(', ')}</p>}
                {canCreate && <fieldset><legend>Platforms</legend>
                    <label className="check"><input type="radio" checked={any} onChange={() => { setAny(true); setPlatforms([]) }} /> Any platform</label>
                    {resolved.Platforms?.map(p => <label className="check" key={p.key}><input type="checkbox" checked={!any && platforms.includes(p.key)} onChange={() => toggle(p.key)} /> {p.name}</label>)}
                    {!platformsValid && <p role="status">Choose at least one platform, or select Any platform.</p>}
                </fieldset>}
                {!authLost && <section className="movieState" aria-label="Request and availability">
                    {success && <p ref={successMessage} tabIndex={-1} role="status"><strong>{success}</strong></p>}
                    <p>{preview.availability === 'available' ? 'Available in your libraries' : preview.availability === 'missing' ? 'Not currently available in your libraries' : 'Library availability unknown'}</p>
                    {preview.request && <><p>Shared Request: {preview.request.status}</p><p role="status">{preview.request.subscribed ? 'Notifications On' : 'You are not subscribed to this Request.'}</p></>}
                    {session.user.role === 'viewer' && !preview.request && <p>Your viewer role cannot create Requests.</p>}
                </section>}
                {canCreate && <><button className="button secondary" aria-expanded={noteOpen} aria-controls="game-note" onClick={() => setNoteOpen(!noteOpen)}>{noteOpen ? 'Hide note' : 'Add a note'}</button>{noteOpen && <label className="field" id="game-note">Note (optional)<textarea maxLength={2000} value={note} disabled={busy} onChange={event => setNote(event.target.value)} /></label>}</>}
                {error && <p role="alert" className="error">{error}</p>}
            </div>
            <footer className="movieFooter" aria-busy={busy}>
                <button className="button secondary" onClick={onClose}>{success ? 'Close' : 'Cancel'}</button>
                {!authLost && preview.request && <a className="button secondary" href={`/requests?requestId=${encodeURIComponent(preview.request.id)}`} onClick={followLink}>View request</a>}
                {!authLost && preview.availability === 'available' && preview.libraries?.map(link => <a key={link.url} className="button" href={link.url} target="_blank" rel="noreferrer">Open in {link.name}</a>)}
                {!authLost && preview.canSubscribe && <button className="button" disabled={busy} onClick={() => void subscribe()}>{busy ? 'Saving…' : 'Notify Me'}</button>}
                {canCreate && <button className="button" disabled={busy || !platformsValid} onClick={() => void create()}>{busy ? 'Submitting…' : 'Create request'}</button>}
                {busy && <span className="sr-only" role="status">Saving your request…</span>}
            </footer>
        </section>
    </div>
}
