import { useEffect, useState, type FormEvent } from 'react'
import { api, ApiError, type LibraryItem, type Selection, type Session } from '../api'
import { followLink } from '../navigation'
import { Modal } from './Modal'

const labels: Record<string, string> = { movie: 'Movie', tv: 'Series', album: 'Album', game: 'Game' }

// One Library item: what it is, where it lives, and the way to report a problem with it (D030).
export function LibraryItemDialog({ item, session, onClose, onReported }: { item: LibraryItem; session: Session; onClose: () => void; onReported: () => void }) {
    const [reporting, setReporting] = useState(false)
    const [kind, setKind] = useState(item.issueKinds[0]?.kind ?? '')
    const [description, setDescription] = useState('')
    const [season, setSeason] = useState('')
    const [episode, setEpisode] = useState('')
    const [busy, setBusy] = useState(false)
    const [error, setError] = useState('')
    const [done, setDone] = useState<{ joined: boolean } | null>(null)
    // An album's tracks come with the single item, not the list.
    const [tracks, setTracks] = useState(item.tracks ?? [])
    const [picked, setPicked] = useState<Set<string>>(new Set())
    useEffect(() => {
        if (item.mediaType !== 'album' || item.tracks) return
        const controller = new AbortController()
        api<LibraryItem>(`/library/items/${encodeURIComponent(item.id)}`, { signal: controller.signal }).then(full => setTracks(full.tracks ?? [])).catch(() => undefined)
        return () => controller.abort()
    }, [item.id, item.mediaType, item.tracks])
    const needsWords = kind === 'other'
    const part = (): Selection | undefined => {
        if (item.mediaType === 'album') return picked.size ? { tracks: tracks.filter(t => picked.has(t.id)).map((t, index) => ({ id: t.id, position: t.position || index + 1, title: t.title })) } : undefined
        if (item.mediaType !== 'tv' || !season) return undefined
        const s = Number(season), e = Number(episode)
        return episode ? { episodes: [{ season: s, episode: e }] } : { seasons: [s] }
    }
    const partValid = item.mediaType !== 'tv' || ((!season || (Number.isInteger(Number(season)) && Number(season) >= 0 && Number(season) <= 999)) && (!episode || (!!season && Number.isInteger(Number(episode)) && Number(episode) >= 1)))
    async function submit(event: FormEvent) {
        event.preventDefault()
        if (busy || !kind || (needsWords && !description.trim()) || !partValid) return
        setBusy(true); setError('')
        try {
            const result = await api<{ joined: boolean }>('/issues', { method: 'POST', body: JSON.stringify({ libraryItemId: item.id, kind, description: description.trim() || undefined, part: part() }) }, session.csrfToken)
            setDone({ joined: !!result?.joined }); onReported()
        } catch (e) {
            setError(e instanceof ApiError && e.status === 429 ? 'You have reported a lot of issues recently. Please try again in a while.' : e instanceof Error ? e.message : 'The report could not be sent.')
        } finally { setBusy(false) }
    }
    return <Modal labelledBy="library-item-title" onClose={onClose}>
        <div className="libItem">
            <span className={`railCover libCover ${item.mediaType === 'album' ? 'square' : item.mediaType === 'game' ? 'game' : ''}`}>
                {item.artworkUrl ? <img src={item.artworkUrl} alt={`Cover for ${item.title}`} /> : <span className="railBlank" aria-hidden="true">{labels[item.mediaType]}</span>}
            </span>
            <div className="libItemBody">
                <h2 id="library-item-title">{item.title}{item.year ? ` (${item.year})` : ''}</h2>
                <p className="libFacts">{[labels[item.mediaType], item.detail, item.source].filter(Boolean).join(' · ')}</p>
                <p className="libFacts">Added {new Date(item.addedAt).toLocaleDateString()}</p>
                {item.openIssues > 0 && <p role="status">{item.openIssues === 1 ? '1 open issue' : `${item.openIssues} open issues`} reported for this item.</p>}
                {item.link && <p><a href={item.link} target="_blank" rel="noreferrer">Open in {item.source}</a></p>}
            </div>
        </div>
        {done ? <div className="libDone" role="status">
            <p><strong>{done.joined ? 'Added to the existing report.' : 'Thanks, your report was sent.'}</strong> An administrator has been told, and you will hear when it is dealt with.</p>
            <div className="draftActions"><a className="button secondary" href="/issues" onClick={event => { followLink(event); onClose() }}>See your issues</a><button className="button" type="button" onClick={onClose}>Done</button></div>
        </div> : reporting ? <form className="libReport" onSubmit={submit}>
            <fieldset className="picker">
                <legend>What is wrong?</legend>
                <div className="pickMode" role="radiogroup" aria-label="Kind of problem">{item.issueKinds.map(option => <label key={option.kind}><input type="radio" name="issue-kind" checked={kind === option.kind} onChange={() => setKind(option.kind)} /> {option.label}</label>)}</div>
            </fieldset>
            {item.mediaType === 'tv' && <div className="libPart">
                <label>Season (optional)<input inputMode="numeric" value={season} onChange={event => setSeason(event.target.value.replace(/\D/g, '').slice(0, 3))} /></label>
                <label>Episode (optional)<input inputMode="numeric" value={episode} disabled={!season} onChange={event => setEpisode(event.target.value.replace(/\D/g, '').slice(0, 5))} /></label>
            </div>}
            {item.mediaType === 'album' && tracks.length > 0 && <fieldset className="picker">
                <legend>Which tracks? (optional)</legend>
                <ul className="pickList" aria-label="Tracks">{tracks.map((track, index) => <li key={track.id}><label>
                    <input type="checkbox" checked={picked.has(track.id)} onChange={() => setPicked(current => { const next = new Set(current); if (!next.delete(track.id)) next.add(track.id); return next })} />
                    <span className="pickIndex">{track.position || index + 1}</span><span className="pickName">{track.title}</span>
                </label></li>)}</ul>
            </fieldset>}
            <label className="field">{needsWords ? 'Describe the problem' : 'Details (optional)'}<textarea maxLength={2000} required={needsWords} value={description} disabled={busy} onChange={event => setDescription(event.target.value)} /></label>
            {error && <p role="alert" className="error">{error}</p>}
            <div className="draftActions">
                <button className="button" type="submit" disabled={busy || !kind || (needsWords && !description.trim()) || !partValid}>{busy ? 'Sending…' : 'Send report'}</button>
                <button className="button secondary" type="button" disabled={busy} onClick={() => setReporting(false)}>Back</button>
            </div>
        </form> : <div className="draftActions"><button className="button secondary" type="button" onClick={() => setReporting(true)}>Report an issue</button></div>}
    </Modal>
}
