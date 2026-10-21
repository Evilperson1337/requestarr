import { useState } from 'react'
import { api, type ResolvedItem, type Session } from '../api'

export function RequestModal({ resolved, session, onCreated, onClose }: { resolved: ResolvedItem; session: Session; onCreated: () => void; onClose: () => void }) {
    const [note, setNote] = useState('')
    const [any, setAny] = useState(true)
    const [platforms, setPlatforms] = useState<string[]>([])
    const [busy, setBusy] = useState(false)
    const [error, setError] = useState('')
    const item = resolved.Item
    async function create() {
        setBusy(true); setError('')
        try { await api('/requests', { method: 'POST', body: JSON.stringify({ catalogId: item.ID, note, platforms: item.MediaType === 'game' ? { any, platformKeys: any ? [] : platforms } : { any: true } }) }, session.csrfToken); onCreated() }
        catch (e) { setError(e instanceof Error ? e.message : 'Could not create the request') }
        finally { setBusy(false) }
    }
    function toggle(key: string) { setAny(false); setPlatforms(current => current.includes(key) ? current.filter(x => x !== key) : [...current, key]) }
    return <div className="overlay" role="presentation" onMouseDown={e => { if (e.target === e.currentTarget) onClose() }}><section className="modal card" role="dialog" aria-modal="true" aria-labelledby="preview-title">
        <button className="close" aria-label="Close preview" onClick={onClose}>×</button><p className="eyebrow">{item.MediaType} preview</p><h2 id="preview-title">{item.Title}{item.Year ? ` (${item.Year})` : ''}</h2>
        <div className="preview">{item.ArtworkURL && <img src={item.ArtworkURL} alt={`Artwork for ${item.Title}`} className={item.ArtworkKind === 'square' ? 'art square' : 'art'} />}<div><p>Provider: {item.Provider}</p>{item.ExternalURL && <a href={item.ExternalURL} target="_blank" rel="noreferrer">Open metadata source</a>}</div></div>
        {item.MediaType === 'game' && <fieldset><legend>Platforms</legend><label className="check"><input type="radio" checked={any} onChange={() => { setAny(true); setPlatforms([]) }} /> Any platform</label>{resolved.Platforms?.map(p => <label className="check" key={p.key}><input type="checkbox" checked={!any && platforms.includes(p.key)} onChange={() => toggle(p.key)} /> {p.name}</label>)}{!any && platforms.length === 0 && <p role="status">Choose at least one platform, or select Any platform.</p>}</fieldset>}
        <label className="field">Note (optional)<textarea maxLength={2000} value={note} onChange={e => setNote(e.target.value)} /></label>
        {error && <p role="alert" className="error">{error}</p>}<div className="actions"><button className="button secondary" onClick={onClose}>Cancel</button><button className="button" disabled={busy || (item.MediaType === 'game' && !any && platforms.length === 0)} onClick={() => void create()}>{busy ? 'Submitting…' : 'Create request'}</button></div>
    </section></div>
}
