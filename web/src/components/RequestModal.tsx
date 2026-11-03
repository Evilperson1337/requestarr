import { useEffect, useRef, useState, type KeyboardEvent } from 'react'
import { api, type ResolvedItem, type Session } from '../api'
import { MovieRequestModal } from './MovieRequestModal'
import { CatalogRequestModal } from './CatalogRequestModal'

export function RequestModal({ resolved, session, onCreated, onClose }: { resolved: ResolvedItem; session: Session; onCreated: () => void; onClose: () => void }) {
    if (['game', 'album', 'song', 'audiobook'].includes(resolved.Item.MediaType)) return <CatalogRequestModal resolved={resolved} session={session} onCreated={onCreated} onClose={onClose} />
    return ['movie', 'tv'].includes(resolved.Item.MediaType) ? <MovieRequestModal resolved={resolved} session={session} onCreated={onCreated} onClose={onClose} /> : <BasicRequestModal resolved={resolved} session={session} onCreated={onCreated} onClose={onClose} />
}

function BasicRequestModal({ resolved, session, onCreated, onClose }: { resolved: ResolvedItem; session: Session; onCreated: () => void; onClose: () => void }) {
    const [note, setNote] = useState('')
    const [any, setAny] = useState(true)
    const [platforms, setPlatforms] = useState<string[]>([])
    const [busy, setBusy] = useState(false)
    const [error, setError] = useState('')
    const item = resolved.Item
    const dialog = useRef<HTMLElement>(null)
    // Move focus into the dialog and give it back to the opener when it closes.
    useEffect(() => {
        const opener = document.activeElement as HTMLElement | null
        dialog.current?.focus()
        return () => opener?.focus()
    }, [])
    function keepFocusInside(event: KeyboardEvent) {
        if (event.key === 'Escape') { event.stopPropagation(); onClose(); return }
        if (event.key !== 'Tab' || !dialog.current) return
        const focusable = [...dialog.current.querySelectorAll<HTMLElement>('button:not(:disabled), input:not(:disabled), select:not(:disabled), textarea:not(:disabled), a[href]')]
        if (focusable.length === 0) return
        const first = focusable[0], last = focusable[focusable.length - 1]
        if (event.shiftKey && (document.activeElement === first || document.activeElement === dialog.current)) { event.preventDefault(); last.focus() }
        else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus() }
    }
    async function create() {
        setBusy(true); setError('')
        try { await api('/requests', { method: 'POST', body: JSON.stringify({ catalogId: item.ID, note, platforms: item.MediaType === 'game' ? { any, platformKeys: any ? [] : platforms } : { any: true } }) }, session.csrfToken); onCreated() }
        catch (e) { setError(e instanceof Error ? e.message : 'Could not create the request') }
        finally { setBusy(false) }
    }
    function toggle(key: string) { setAny(false); setPlatforms(current => current.includes(key) ? current.filter(x => x !== key) : [...current, key]) }
    return <div className="overlay" role="presentation" onMouseDown={e => { if (e.target === e.currentTarget) onClose() }}><section ref={dialog} tabIndex={-1} onKeyDown={keepFocusInside} className="modal card" role="dialog" aria-modal="true" aria-labelledby="preview-title">
        <button className="close" aria-label="Close preview" onClick={onClose}>×</button><p className="eyebrow">{item.MediaType} preview</p><h2 id="preview-title">{item.Title}{item.Year ? ` (${item.Year})` : ''}</h2>
        <div className="preview">{item.ArtworkURL && <img src={item.ArtworkURL} alt={`Artwork for ${item.Title}`} className={item.ArtworkKind === 'square' ? 'art square' : 'art'} />}<div><p>Provider: {item.Provider}</p>{item.ExternalURL && <a href={item.ExternalURL} target="_blank" rel="noreferrer">Open metadata source</a>}</div></div>
        {item.MediaType === 'game' && <fieldset><legend>Platforms</legend><label className="check"><input type="radio" checked={any} onChange={() => { setAny(true); setPlatforms([]) }} /> Any platform</label>{resolved.Platforms?.map(p => <label className="check" key={p.key}><input type="checkbox" checked={!any && platforms.includes(p.key)} onChange={() => toggle(p.key)} /> {p.name}</label>)}{!any && platforms.length === 0 && <p role="status">Choose at least one platform, or select Any platform.</p>}</fieldset>}
        <label className="field">Note (optional)<textarea maxLength={2000} value={note} onChange={e => setNote(e.target.value)} /></label>
        {error && <p role="alert" className="error">{error}</p>}<div className="actions"><button className="button secondary" onClick={onClose}>Cancel</button><button className="button" disabled={busy || (item.MediaType === 'game' && !any && platforms.length === 0)} onClick={() => void create()}>{busy ? 'Submitting…' : 'Create request'}</button></div>
    </section></div>
}
