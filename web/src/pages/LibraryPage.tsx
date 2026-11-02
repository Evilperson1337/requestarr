import { useCallback, useEffect, useRef, useState } from 'react'
import { api, type LibraryItem, type LibraryPage as Page, type Session } from '../api'
import { LibraryItemDialog } from '../components/LibraryItemDialog'

const tabs = [['movie', 'Movies'], ['tv', 'TV'], ['album', 'Music'], ['game', 'Games']] as const
const sources = [['', 'All libraries'], ['plex', 'Plex'], ['jellyfin', 'Jellyfin'], ['navidrome', 'Navidrome'], ['romm', 'RomM']] as const
const cover = (type: string) => type === 'album' ? ' square' : type === 'game' ? ' game' : ''

// What the libraries hold, one media type at a time, each with its own cover shape (D030).
export function LibraryPage({ session }: { session: Session }) {
    const [type, setType] = useState<(typeof tabs)[number][0]>('movie')
    const [draft, setDraft] = useState('')
    const [search, setSearch] = useState('')
    const [source, setSource] = useState('')
    const [items, setItems] = useState<LibraryItem[]>([])
    const [next, setNext] = useState<string | null>(null)
    const [loading, setLoading] = useState(true)
    const [error, setError] = useState('')
    const [open, setOpen] = useState<LibraryItem | null>(null)
    const generation = useRef(0)
    useEffect(() => { const t = setTimeout(() => setSearch(draft.trim()), 300); return () => clearTimeout(t) }, [draft])
    const load = useCallback(async (cursor: string | null) => {
        const mine = ++generation.current
        setLoading(true); setError('')
        const params = new URLSearchParams({ type, limit: '48' })
        if (search) params.set('q', search)
        if (source) params.set('source', source)
        if (cursor) params.set('cursor', cursor)
        try {
            const page = await api<Page>(`/library/items?${params}`, { cache: 'no-store' })
            if (mine !== generation.current) return
            setItems(current => cursor ? [...current, ...page.items] : page.items); setNext(page.nextCursor)
        } catch (e) { if (mine === generation.current) setError(e instanceof Error ? e.message : 'The library could not be loaded.') }
        finally { if (mine === generation.current) setLoading(false) }
    }, [type, search, source])
    useEffect(() => { setItems([]); setNext(null); void load(null) }, [load])
    return <main className="shell libraryPage">
        <h1>Library</h1>
        <div className="settingsTabs" role="group" aria-label="Media type">{tabs.map(([key, label]) => <button key={key} type="button" className="settingsTab" aria-pressed={type === key} onClick={() => setType(key)}>{label}</button>)}</div>
        <div className="libraryFilters">
            <label className="sr-only" htmlFor="library-search">Search the library</label>
            <input id="library-search" type="search" placeholder="Search by title…" value={draft} onChange={event => setDraft(event.target.value)} maxLength={100} />
            <label className="sr-only" htmlFor="library-source">Library</label>
            <select id="library-source" value={source} onChange={event => setSource(event.target.value)}>{sources.map(([key, label]) => <option key={key} value={key}>{label}</option>)}</select>
        </div>
        {error && <p role="alert" className="error">{error}</p>}
        {!loading && !error && items.length === 0 && <p>{search || source ? 'Nothing matches that.' : 'Nothing here yet. Items appear once a library has synced.'}</p>}
        <ul className="coverRail libraryGrid" aria-label={tabs.find(t => t[0] === type)?.[1]} aria-busy={loading}>{items.map(item => <li key={item.id}>
            <button type="button" className="railCard libraryCard" onClick={() => setOpen(item)}>
                <span className={`railCover${cover(item.mediaType)}`}>
                    {item.artworkUrl ? <img src={item.artworkUrl} alt="" loading="lazy" /> : <span className="railBlank" aria-hidden="true">{tabs.find(t => t[0] === item.mediaType)?.[1]}</span>}
                    {item.openIssues > 0 && <span className="railStatus status-denied" aria-hidden="true">{item.openIssues}</span>}
                </span>
                <span className="railTitle">{item.title}</span>
                <span className="railKind">{[item.year || '', item.detail || '', item.source].filter(Boolean).join(' · ')}</span>
                {item.openIssues > 0 && <span className="sr-only">{item.openIssues} open {item.openIssues === 1 ? 'issue' : 'issues'}</span>}
            </button>
        </li>)}</ul>
        {loading && <p role="status">Loading…</p>}
        {next && !loading && <div className="draftActions"><button className="button secondary" type="button" onClick={() => void load(next)}>Show more</button></div>}
        {open && <LibraryItemDialog item={open} session={session} onClose={() => setOpen(null)} onReported={() => void load(null)} />}
    </main>
}
