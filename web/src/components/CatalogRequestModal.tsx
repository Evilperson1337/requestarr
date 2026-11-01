import { useEffect, useRef, useState, type KeyboardEvent, type ReactNode } from 'react'
import { api, ApiError, type MoviePreviewState, type ResolvedItem, type Session } from '../api'
import { followLink } from '../navigation'
import { LibraryTags } from './LibraryTags'

// What the confirmation screen shows for one catalog item, whatever its media
// type. Each media type fills it from its own provider details.
type Facts = {
    eyebrow: string; noun: string; source: string; tagLabel: string
    backdropUrl?: string; badges: string[]; upcoming: boolean
    lines: ReactNode[]; rating: ReactNode
    overview: string; credit?: { label: string; names: string[] }
    alsoOn?: { label: string; names: string[] }
    needsPlatforms: boolean
}

const longDate = (iso?: string) => {
    if (!iso || !/^\d{4}-\d{2}-\d{2}$/.test(iso)) return null
    // Parse date-only values at local noon so locale formatting never shifts a day.
    const date = new Date(`${iso}T12:00:00`)
    return Number.isNaN(date.getTime()) ? null : <time dateTime={iso}>{date.toLocaleDateString(undefined, { year: 'numeric', month: 'long', day: 'numeric' })}</time>
}
const todayISO = () => new Date().toLocaleDateString('en-CA')

function describe(resolved: ResolvedItem): Facts {
    const item = resolved.Item
    if (item.MediaType === 'game') {
        const game = resolved.Game
        const date = longDate(game?.releaseDate)
        return {
            eyebrow: 'Game preview', noun: 'game', source: 'IGDB', tagLabel: 'Genres', backdropUrl: game?.backdropUrl, badges: game?.genres ?? [],
            upcoming: !!game?.releaseDate && game.releaseDate > todayISO(),
            lines: [date ? <><strong>Release date:</strong> {date}</> : 'Release date unknown'],
            rating: game?.ratingCount && game.score !== undefined ? <><strong>{Math.round(game.score)}/100</strong> · {game.ratingCount.toLocaleString()} ratings</> : '· Not yet rated',
            overview: game?.overview ?? '', credit: game?.developers?.length ? { label: game.developers.length > 1 ? 'Developers' : 'Developer', names: game.developers } : undefined,
            needsPlatforms: true,
        }
    }
    const music = resolved.Music
    const isSong = item.MediaType === 'song'
    const date = longDate(music?.releaseDate)
    const length = music?.lengthSeconds ? `${Math.floor(music.lengthSeconds / 60)}:${String(music.lengthSeconds % 60).padStart(2, '0')}` : ''
    return {
        eyebrow: isSong ? 'Song preview' : 'Album preview', noun: isSong ? 'song' : 'album', source: 'MusicBrainz', tagLabel: 'Type and genres',
        badges: [music?.primaryType, ...(music?.secondaryTypes ?? []), ...(music?.genres ?? [])].filter((x): x is string => !!x),
        upcoming: !!music?.releaseDate && music.releaseDate > todayISO(),
        lines: [date ? <><strong>Release date:</strong> {date}</> : item.Year ? <><strong>Released:</strong> {item.Year}</> : 'Release date unknown', ...(length ? [<><strong>Length:</strong> {length}</>] : [])],
        rating: music?.ratingCount && music.score !== undefined ? <><strong>{music.score.toFixed(1)}/5</strong> · {music.ratingCount.toLocaleString()} ratings</> : '· Not yet rated',
        overview: '', credit: music?.artists?.length ? { label: music.artists.length > 1 ? 'Artists' : 'Artist', names: music.artists } : undefined,
        alsoOn: music?.appearsOn?.length ? { label: 'Appears on', names: music.appearsOn } : undefined,
        needsPlatforms: false,
    }
}

export function CatalogRequestModal({ resolved, session, onClose }: { resolved: ResolvedItem; session: Session; onCreated: () => void; onClose: () => void }) {
    const item = resolved.Item
    const facts = describe(resolved)
    const dialog = useRef<HTMLElement>(null)
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
    const platformsValid = !facts.needsPlatforms || any || platforms.length > 0
    const canCreate = !authLost && !success && preview.canRequest && !preview.request && preview.availability !== 'available' && session.user.role !== 'viewer'
    async function create() {
        if (pending.current || !canCreate || !platformsValid) return
        const controller = new AbortController(); pending.current = controller
        setBusy(true); setError('')
        try {
            const result = await api<{ Created: boolean; Request: { ID: string; Status: string } }>('/requests', { method: 'POST', signal: controller.signal, body: JSON.stringify({ catalogId: item.ID, note, platforms: facts.needsPlatforms ? { any, platformKeys: any ? [] : platforms } : { any: true } }) }, session.csrfToken)
            if (controller.signal.aborted) return
            if (!result?.Request?.ID) throw new Error('The request result could not be confirmed. Retry safely to check the shared Request.')
            setPreview({ availability: preview.availability, canRequest: false, canSubscribe: !result.Created && ['requested', 'approved'].includes(result.Request.Status), request: { id: result.Request.ID, status: result.Request.Status, subscribed: result.Created } })
            setSuccess(result.Created ? `${facts.noun[0].toUpperCase()}${facts.noun.slice(1)} requested` : `This ${facts.noun} was already requested`)
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
    const overview = facts.overview
    // Album and single art is square: it keeps its own shape, and with no backdrop it also fills the banner, blurred.
    const square = item.ArtworkKind === 'square'
    const banner = facts.backdropUrl && !backdropFailed ? facts.backdropUrl : square && item.ArtworkURL && !posterFailed ? item.ArtworkURL : ''
    const fromCover = !!banner && banner === item.ArtworkURL && banner !== facts.backdropUrl
    const hasActions = canCreate || (!authLost && (!!preview.request || preview.canSubscribe))
    const source = item.ExternalURL ? <a href={item.ExternalURL} target="_blank" rel="noreferrer" aria-label={`${facts.source} (opens in a new tab)`}>{facts.source}</a> : facts.source
    return <div className="overlay movieOverlay" onMouseDown={event => { if (event.target === event.currentTarget) onClose() }}>
        <section ref={dialog} tabIndex={-1} onKeyDown={keyboard} role="dialog" aria-modal="true" aria-labelledby="catalog-preview-title" className="movieModal">
            <button className="close movieClose" aria-label="Close preview" onClick={onClose}>×</button>
            <span className="movieKind">{facts.eyebrow}</span>
            <div className="movieScroll">
                {banner && <div className="movieBanner"><img className={fromCover ? 'bannerImg fromCover' : 'bannerImg'} src={banner} alt="" onError={() => fromCover ? setPosterFailed(true) : setBackdropFailed(true)} /></div>}
                <div className={`movieIdentity${banner ? ' overlap' : ''}${square ? ' square' : ''}`}>
                    {item.ArtworkURL && !posterFailed ? <img className="moviePoster" src={item.ArtworkURL} alt={`${item.MediaType === 'game' ? 'Cover' : 'Cover art'} for ${item.Title}`} onError={() => setPosterFailed(true)} /> : <div className="moviePoster moviePlaceholder" role="img" aria-label="Cover unavailable">Cover unavailable</div>}
                    <div className="movieHeading"><h2 id="catalog-preview-title">{item.Title}{item.Year ? ` (${item.Year})` : ''}</h2>
                        {facts.credit && <p className="movieCredit"><span className="sr-only">{facts.credit.label}: </span>{facts.credit.names.join(', ')}</p>}
                        <div className="movieFacts">{facts.upcoming && <span className="movieBadge">Upcoming</span>}</div>
                        {facts.badges.length > 0 && <ul className="tags" aria-label={facts.tagLabel}>{facts.badges.map((badge, index) => <li className="tag" key={`${badge}-${index}`}>{badge}</li>)}</ul>}
                        {facts.lines.map((line, index) => <p key={index}>{line}</p>)}
                        <p className="movieRating">{source} {facts.rating}</p>
                        {!authLost && <LibraryTags preview={preview} />}
                    </div>
                </div>
                {overview && <section aria-label="Synopsis"><h3>Synopsis</h3><p>{overview}</p></section>}
                {facts.alsoOn && <p><strong>{facts.alsoOn.label}:</strong> {facts.alsoOn.names.join(', ')}</p>}
                {canCreate && facts.needsPlatforms && <fieldset><legend>Platforms</legend>
                    <label className="check"><input type="radio" checked={any} onChange={() => { setAny(true); setPlatforms([]) }} /> Any platform</label>
                    {resolved.Platforms?.map(p => <label className="check" key={p.key}><input type="checkbox" checked={!any && platforms.includes(p.key)} onChange={() => toggle(p.key)} /> {p.name}</label>)}
                    {!platformsValid && <p role="status">Choose at least one platform, or select Any platform.</p>}
                </fieldset>}
                {!authLost && <section className="movieState" aria-label="Request and availability">
                    {success && <p ref={successMessage} tabIndex={-1} role="status"><strong>{success}</strong></p>}
                    {preview.request && <><p>Shared Request: {preview.request.status}</p><p role="status">{preview.request.subscribed ? 'Notifications On' : 'You are not subscribed to this Request.'}</p></>}
                    {session.user.role === 'viewer' && !preview.request && <p>Your viewer role cannot create Requests.</p>}
                </section>}
                {canCreate && <><button className="button secondary" aria-expanded={noteOpen} aria-controls="catalog-note" onClick={() => setNoteOpen(!noteOpen)}>{noteOpen ? 'Hide note' : 'Add a note'}</button>{noteOpen && <label className="field" id="catalog-note">Note (optional)<textarea maxLength={2000} value={note} disabled={busy} onChange={event => setNote(event.target.value)} /></label>}</>}
                {error && <p role="alert" className="error">{error}</p>}
            </div>
            {hasActions && <footer className="movieFooter" aria-busy={busy}>
                {!authLost && preview.request && <a className="button secondary" href={`/requests?requestId=${encodeURIComponent(preview.request.id)}`} onClick={followLink}>View request</a>}
                                {!authLost && preview.canSubscribe && <button className="button" disabled={busy} onClick={() => void subscribe()}>{busy ? 'Saving…' : 'Notify Me'}</button>}
                {canCreate && <button className="button" disabled={busy || !platformsValid} onClick={() => void create()}>{busy ? 'Submitting…' : 'Create request'}</button>}
                {busy && <span className="sr-only" role="status">Saving your request…</span>}
            </footer>}
        </section>
    </div>
}
