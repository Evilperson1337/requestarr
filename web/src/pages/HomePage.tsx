import { useState, type FormEvent } from 'react'
import { api, type ResolvedItem, type Session } from '../api'
import { RequestModal } from '../components/RequestModal'

export function HomePage({ session, onCreated }: { session: Session; onCreated: () => void }) {
    const [url, setURL] = useState('')
    const [item, setItem] = useState<ResolvedItem | null>(null)
    const [busy, setBusy] = useState(false)
    const [error, setError] = useState('')
    async function resolve(e: FormEvent) {
        e.preventDefault(); setBusy(true); setError(''); setItem(null)
        try { setItem(await api<ResolvedItem>('/resolve', { method: 'POST', body: JSON.stringify({ url }) }, session.csrfToken)) }
        catch (e) { setError(e instanceof Error ? e.message : 'Could not resolve that link') }
        finally { setBusy(false) }
    }
    return <main className="shell">
        <section className="card hero"><p className="eyebrow">Requestarr · Your shared library</p><h1>Request Something</h1><p>Paste a TMDB, MusicBrainz, or IGDB link to preview it before requesting.</p>
            <form onSubmit={resolve}><label className="urlBox"><span aria-hidden="true">↗</span><span className="sr-only">TMDB, MusicBrainz, or IGDB URL</span><input type="url" required value={url} onChange={e => setURL(e.target.value)} placeholder="Paste a supported link…" /></label><button className="button" disabled={busy}>{busy ? 'Resolving…' : 'Preview'}</button></form>
            {busy && <p role="status">Looking up metadata…</p>}{error && <p role="alert" className="error">{error}</p>}
            <div className="providers"><span>TMDB · Movies &amp; TV</span><span>MusicBrainz · Music</span><span>IGDB · Games</span></div>
        </section>
        {item && <RequestModal resolved={item} session={session} onCreated={onCreated} onClose={() => setItem(null)} />}
    </main>
}
