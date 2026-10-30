import { useEffect, useRef, useState, type KeyboardEvent } from 'react'
import { api, ApiError, type MoviePreviewState, type ResolvedItem, type Session } from '../api'
import { followLink } from '../navigation'
import { LibraryTags } from './LibraryTags'

export function MovieRequestModal({ resolved, session, onClose }: { resolved: ResolvedItem; session: Session; onCreated: () => void; onClose: () => void }) {
    const item = resolved.Item, tv = resolved.TV
    const isTV = item.MediaType === 'tv'
    const movie = isTV ? tv && { ...tv, runtime: tv.episodeRuntime, releaseDate: tv.firstAirDate, releaseStatus: tv.seriesStatus, directors: tv.creators } : resolved.Movie
    const requestLabel = isTV ? 'Request series' : 'Request movie'
    const dialog = useRef<HTMLElement>(null)
    const [posterFailed, setPosterFailed] = useState(false)
    const [backdropFailed, setBackdropFailed] = useState(false)
    const [note, setNote] = useState('')
    const [noteOpen, setNoteOpen] = useState(false)
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
    async function create() {
        if (pending.current || !canCreate) return
        const controller = new AbortController(); pending.current = controller
        setBusy(true); setError('')
        try {
            const result = await api<{ Created: boolean; Request: { ID: string; Status: string } }>('/requests', { method: 'POST', signal: controller.signal, body: JSON.stringify({ catalogId: item.ID, note, platforms: { any: true } }) }, session.csrfToken)
            if (controller.signal.aborted) return
            if (!result?.Request?.ID) throw new Error('The request result could not be confirmed. Retry safely to check the shared Request.')
            setPreview({ availability: preview.availability, canRequest: false, canSubscribe: !result.Created && ['requested', 'approved'].includes(result.Request.Status), request: { id: result.Request.ID, status: result.Request.Status, subscribed: result.Created } })
            setSuccess(result.Created ? isTV ? 'Series requested' : 'Movie requested' : isTV ? 'This series was already requested' : 'This movie was already requested')
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
    const canCreate = !authLost && !success && preview.canRequest && !preview.request && preview.availability !== 'available' && session.user.role !== 'viewer'
    const overview = movie?.overview || ''
    // Parse date-only values at local noon so locale formatting never shifts a day.
    const releaseDate = movie?.releaseDate && /^\d{4}-\d{2}-\d{2}$/.test(movie.releaseDate) ? new Date(`${movie.releaseDate}T12:00:00`) : null
    const today = new Date(); const todayISO = `${today.getFullYear()}-${String(today.getMonth() + 1).padStart(2, '0')}-${String(today.getDate()).padStart(2, '0')}`
    const upcoming = isTV
        ? !['Ended', 'Canceled'].includes(tv?.seriesStatus || '') && (releaseDate ? movie!.releaseDate! > todayISO : ['Planned', 'In Production', 'Pilot'].includes(tv?.seriesStatus || ''))
        : movie?.releaseStatus !== 'Canceled' && (releaseDate ? movie!.releaseDate! > todayISO : movie?.releaseStatus && movie.releaseStatus !== 'Released')
    const lastAirDate = tv?.lastAirDate && /^\d{4}-\d{2}-\d{2}$/.test(tv.lastAirDate) ? new Date(`${tv.lastAirDate}T12:00:00`) : null
    return <div className="overlay movieOverlay" onMouseDown={event => { if (event.target === event.currentTarget) onClose() }}>
        <section ref={dialog} tabIndex={-1} onKeyDown={keyboard} role="dialog" aria-modal="true" aria-labelledby="movie-preview-title" className="movieModal">
            <div className="movieScroll">
                {movie?.backdropUrl && !backdropFailed && <img className="movieBackdrop" src={movie.backdropUrl} alt="" onError={() => setBackdropFailed(true)} />}
                <header className="movieHeader"><p className="eyebrow">{isTV ? 'TV preview' : 'Movie preview'}</p><button className="close" aria-label="Close preview" onClick={onClose}>×</button></header>
                <div className="movieIdentity">
                    {item.ArtworkURL && !posterFailed ? <img className="moviePoster" src={item.ArtworkURL} alt={`Poster for ${item.Title}`} onError={() => setPosterFailed(true)} /> : <div className="moviePoster moviePlaceholder" role="img" aria-label="Poster unavailable">Poster unavailable</div>}
                    <div className="movieHeading"><h2 id="movie-preview-title">{item.Title}{item.Year ? ` (${item.Year})` : ''}</h2>
                        <div className="movieFacts">{upcoming ? <span className="movieBadge">Upcoming</span> : movie?.releaseStatus && <span className="movieBadge">{movie.releaseStatus}</span>}{movie?.runtime ? <span>{Math.floor(movie.runtime / 60) ? `${Math.floor(movie.runtime / 60)}h ` : ''}{movie.runtime % 60}m{isTV ? ' per episode' : ''}</span> : null}{isTV && tv?.seasons ? <span>{tv.seasons.toLocaleString()} {tv.seasons === 1 ? 'season' : 'seasons'}</span> : null}{isTV && tv?.episodes ? <span>{tv.episodes.toLocaleString()} {tv.episodes === 1 ? 'episode' : 'episodes'}</span> : null}</div>
                        {!!movie?.genres?.length && <ul className="tags" aria-label="Genres">{movie.genres.map((genre, index) => <li className="tag" key={`${genre}-${index}`}>{genre}</li>)}</ul>}
                        <p>{releaseDate && !Number.isNaN(releaseDate.getTime()) ? <><strong>{isTV ? 'First aired:' : 'Release date:'}</strong> <time dateTime={movie?.releaseDate}>{releaseDate.toLocaleDateString(undefined, { year: 'numeric', month: 'long', day: 'numeric' })}</time></> : isTV ? 'First air date unknown' : 'Release date unknown'}</p>
                        {isTV && lastAirDate && !Number.isNaN(lastAirDate.getTime()) && <p><strong>Last aired:</strong> <time dateTime={tv?.lastAirDate}>{lastAirDate.toLocaleDateString(undefined, { year: 'numeric', month: 'long', day: 'numeric' })}</time></p>}
                        {isTV && <p>Requests cover the whole series.</p>}
                        <p className="movieRating">{movie?.ratingCount && movie.score !== undefined ? <>TMDB <strong>{movie.score.toFixed(1)}/10</strong> · {movie.ratingCount.toLocaleString()} ratings</> : 'TMDB · Not yet rated'}</p>
                        {item.ExternalURL && <a href={item.ExternalURL} target="_blank" rel="noreferrer">Open on TMDB ↗</a>}
                        {!authLost && <LibraryTags preview={preview} />}
                    </div>
                </div>
                {overview && <section aria-label="Synopsis"><h3>Synopsis</h3><p>{overview}</p></section>}
                {!!movie?.directors?.length && <p><strong>{isTV ? 'Creator' : 'Director'}{movie.directors.length > 1 ? 's' : ''}:</strong> {movie.directors.join(', ')}</p>}
                {!!movie?.cast?.length && <section aria-label="Lead cast"><h3>Lead cast</h3><ul className="movieCast">{movie.cast.slice(0, 5).map((person, index) => <li key={`${person.name}-${index}`}><strong>{person.name}</strong>{person.character && <span>{person.character}</span>}</li>)}</ul></section>}
                {movie?.detailsUnavailable && <p role="status">Some details unavailable.</p>}
                {!authLost && <section className="movieState" aria-label="Request and availability">
                    {success && <p ref={successMessage} tabIndex={-1} role="status"><strong>{success}</strong></p>}
                    {preview.request && <><p>Shared Request: {preview.request.status}</p><p role="status">{preview.request.subscribed ? 'Notifications On' : 'You are not subscribed to this Request.'}</p></>}
                    {session.user.role === 'viewer' && !preview.request && <p>Your viewer role cannot create Requests.</p>}
                </section>}
                {canCreate && <><button className="button secondary" aria-expanded={noteOpen} aria-controls="movie-note" onClick={() => setNoteOpen(!noteOpen)}>{noteOpen ? 'Hide note' : 'Add a note'}</button>{noteOpen && <label className="field" id="movie-note">Note (optional)<textarea maxLength={2000} value={note} disabled={busy} onChange={event => setNote(event.target.value)} /></label>}</>}
                {error && <p role="alert" className="error">{error}</p>}
            </div>
            <footer className="movieFooter" aria-busy={busy}>
                <button className="button secondary" onClick={onClose}>{success ? 'Close' : 'Cancel'}</button>
                {!authLost && preview.request && <a className="button secondary" href={`/requests?requestId=${encodeURIComponent(preview.request.id)}`} onClick={followLink}>View request</a>}
                                {!authLost && preview.canSubscribe && <button className="button" disabled={busy} onClick={() => void subscribe()}>{busy ? 'Saving…' : 'Notify Me'}</button>}
                {canCreate && <button className="button" disabled={busy} onClick={() => void create()}>{busy ? 'Submitting…' : requestLabel}</button>}
                {busy && <span className="sr-only" role="status">Saving your request…</span>}
            </footer>
        </section>
    </div>
}
