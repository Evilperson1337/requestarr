import { useCallback, useEffect, useRef, useState, type FormEvent } from 'react'
import { api, type MoviePreviewState, type SearchPage, type SearchResult, type RecentLibraryItem, type RequestItem, type RequestPage, type ResolvedItem, type Session } from '../api'
import { LibraryRail, RequestRail } from '../components/RequestRail'
import { RequestModal } from '../components/RequestModal'
import { followLink } from '../navigation'
import { isLinkInput, searchBadges, searchTypeLabels, typeaheadTypes, useSearchTypes, type SearchState } from '../search'

export function HomePage({ session, onCreated }: { session: Session; onCreated: () => void }) {
    const [text, setText] = useState('')
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
            const page = await api<RequestPage>('/requests?mine=true&limit=12', { cache: 'no-store', signal })
            if (!signal?.aborted) { setRecent(Array.isArray(page?.items) ? page.items : []); setRecentFailed(false) }
        } catch { if (!signal?.aborted) setRecentFailed(true) }
    }, [])
    useEffect(() => { const controller = new AbortController(); void loadRecent(controller.signal); return () => controller.abort() }, [session.user.id, loadRecent])
    // Shared and optional: what has just become available. A failure simply leaves the row out.
    const [available, setAvailable] = useState<RequestItem[]>([])
    const [addedVideo, setAddedVideo] = useState<RecentLibraryItem[]>([])
    const [addedMusic, setAddedMusic] = useState<RecentLibraryItem[]>([])
    const [addedGames, setAddedGames] = useState<RecentLibraryItem[]>([])
    const [addedBooks, setAddedBooks] = useState<RecentLibraryItem[]>([])
    useEffect(() => {
        const controller = new AbortController()
        api<RequestPage>('/requests?status=available&limit=12', { cache: 'no-store', signal: controller.signal })
            .then(page => { if (!controller.signal.aborted) setAvailable(Array.isArray(page?.items) ? page.items : []) })
            .catch(() => undefined)
        return () => controller.abort()
    }, [session.user.id])
    useEffect(() => {
        const controller = new AbortController()
        // One shelf per cover shape, so every cover in a row has the same proportions.
        const shelf = (types: string, set: (items: RecentLibraryItem[]) => void) => api<{ items: RecentLibraryItem[] }>(`/library/recent?types=${types}&limit=48`, { signal: controller.signal })
            .then(result => { if (!controller.signal.aborted) set(Array.isArray(result?.items) ? result.items : []) })
            .catch(() => undefined)
        void shelf('movie,tv', setAddedVideo); void shelf('album', setAddedMusic); void shelf('game', setAddedGames); void shelf('audiobook', setAddedBooks)
        return () => controller.abort()
    }, [session.user.id])
    // Search state is kept apart from the link-resolve state above.
    const { types, searchType, chooseType } = useSearchTypes()
    const [search, setSearch] = useState<SearchState>({ status: 'idle', query: '', results: [], error: '' })
    const searchController = useRef<AbortController | null>(null)
    const searched = useRef('')
    useEffect(() => () => searchController.current?.abort(), [])
    const link = isLinkInput(text)
    const runSearch = useCallback(async (query: string, type: string) => {
        searchController.current?.abort()
        const controller = new AbortController(); searchController.current = controller
        searched.current = `${type}:${query}`
        setSearch(previous => ({ ...previous, status: 'loading', error: '' }))
        try {
            const page = await api<SearchPage>(`/search?${new URLSearchParams({ q: query, type })}`, { signal: controller.signal, cache: 'no-store' })
            if (searchController.current === controller && !controller.signal.aborted) setSearch({ status: 'done', query, results: Array.isArray(page?.results) ? page.results : [], hasMore: !!page?.hasMore, error: '' })
        } catch (e) {
            if (searchController.current === controller && !controller.signal.aborted) setSearch({ status: 'error', query, results: [], error: `${e instanceof Error ? e.message : 'The search failed.'} You can still paste a link.` })
        }
    }, [])
    // Typeahead: search once typing pauses. Anything newer cancels what is in flight, so a slow answer can never land late.
    useEffect(() => {
        const query = text.trim()
        searchController.current?.abort()
        if (link || [...query].length < 2 || !searchType || !typeaheadTypes.has(searchType)) {
            searched.current = ''
            setSearch(previous => previous.status === 'idle' ? previous : { status: 'idle', query: '', results: [], error: '' })
            return
        }
        const key = `${searchType}:${query}`
        if (searched.current === key) return
        const timer = setTimeout(() => void runSearch(query, searchType), 300)
        return () => clearTimeout(timer)
    }, [text, link, searchType, runSearch])
    function submit(e: FormEvent) {
        e.preventDefault()
        if (link) { void resolve(text, document.activeElement instanceof HTMLElement ? document.activeElement : previewButton.current); return }
        const query = text.trim()
        if ([...query].length < 2) { setSearch({ status: 'error', query, results: [], error: 'Type at least 2 characters to search, or paste a link.' }); return }
        if (!searchType) { setSearch({ status: 'error', query, results: [], error: 'Searching by name is not set up here. You can still paste a link.' }); return }
        void runSearch(query, searchType)
    }
    async function resolve(raw: string, opener: HTMLElement | null) {
        if (busy) return
        returnFocus.current = opener ?? previewButton.current
        setBusy(true); setError(''); setItem(null)
        lookup.current?.abort()
        const controller = new AbortController(); lookup.current = controller
        const key = `${session.user.id}:${session.user.role}:${raw.trim()}`
        try {
            let result: ResolvedItem
            if (cached.current?.key === key && ['movie', 'tv'].includes(cached.current.item.Item.MediaType)) {
                const state = await api<MoviePreviewState>(`/catalog/${encodeURIComponent(cached.current.item.Item.ID)}/preview`, { signal: controller.signal, cache: 'no-store' })
                result = { ...cached.current.item, Preview: state }
            } else result = await api<ResolvedItem>('/resolve', { method: 'POST', signal: controller.signal, body: JSON.stringify({ url: raw }) }, session.csrfToken)
            if (!controller.signal.aborted) { cached.current = { key, item: result }; setItem(result) }
        }
        catch (e) { if (!controller.signal.aborted) { setError(e instanceof Error ? e.message : 'Could not resolve that link'); cached.current = null } }
        finally { if (!controller.signal.aborted) setBusy(false) }
    }
    const searchStatus = search.status === 'loading' ? 'Searching…'
        : search.status === 'done' ? (search.results.length === 0 ? `No results for ${search.query}. Try another spelling, or paste a link.` : `${search.results.length} ${search.results.length === 1 ? 'result' : 'results'} for ${search.query}`)
        : ''
    return <main className="shell">
        <section className="card hero"><h1>Request Something</h1><p>{types.includes('movie_tv') ? 'Search for a movie or show by name, or paste' : 'Paste'} a TMDB, MusicBrainz, IGDB or Audible link, or an audiobook ASIN, to preview it before requesting.</p>
            <form onSubmit={submit}><label className="urlBox"><span aria-hidden="true">↗</span><span className="sr-only">Search by name, or paste a TMDB, MusicBrainz, IGDB or Audible URL, or an audiobook ASIN</span><input type="text" inputMode="search" autoComplete="off" spellCheck={false} maxLength={2048} required value={text} onChange={e => setText(e.target.value)} placeholder="Search or paste a link…" /></label><button ref={previewButton} className="button" disabled={busy} onClick={event => { if (event.detail > 0) event.currentTarget.focus() }}>{busy ? 'Resolving…' : link || !text.trim() ? 'Preview' : 'Search'}</button></form>
            {types.length > 0 && <fieldset className="searchTypes" role="radiogroup"><legend>Search for</legend>{types.map(type => <label key={type}><input type="radio" name="searchType" value={type} checked={searchType === type} onChange={() => chooseType(type)} />{searchTypeLabels[type] ?? type}</label>)}</fieldset>}
            {busy && <p role="status">Looking up metadata…</p>}{error && <p role="alert" className="error">{error}</p>}
            {search.status === 'error' && <p role="alert" className="error">{search.error}</p>}
            <p role="status" className="searchStatus">{searchStatus}</p>
            {search.results.length > 0 && <ul className="searchResults" aria-label="Search results" aria-busy={search.status === 'loading'}>{search.results.map(result => <SearchRow key={result.ref} result={result} onPick={opener => void resolve(result.ref, opener)} />)}</ul>}
            {search.status === 'done' && search.hasMore && <p className="searchMore">There are more matches. Add more of the title to narrow them down.</p>}
            <ul className="providers" aria-label="Supported links"><li>TMDB · Movies &amp; TV</li><li>MusicBrainz · Music</li><li>IGDB · Games</li><li>Audible · Audiobooks (link or ASIN)</li></ul>
        </section>
        <section className="recent" aria-labelledby="recent-heading">
            <div className="recentHeader"><h2 id="recent-heading">Your recent requests</h2><a href="/requests?mine=true" onClick={followLink}>Open the full queue</a></div>
            {recent === null ? <p>{recentFailed ? 'Your recent requests could not be loaded.' : 'Loading…'}</p> : recent.length === 0 ? <p>Nothing yet. Paste a link above to make your first request.</p> : <RequestRail items={recent} />}
        </section>
        {available.length > 0 && <section className="recent" aria-labelledby="available-heading">
            <div className="recentHeader"><h2 id="available-heading">Recently available</h2><a href="/requests?status=available" onClick={followLink}>All available</a></div>
            <RequestRail items={available} />
        </section>}
        {addedVideo.length > 0 && <section className="recent" aria-labelledby="added-video-heading">
            <div className="recentHeader"><h2 id="added-video-heading">New movies and shows in your libraries</h2></div>
            <LibraryRail items={addedVideo} label="movies and shows" />
        </section>}
        {addedMusic.length > 0 && <section className="recent" aria-labelledby="added-music-heading">
            <div className="recentHeader"><h2 id="added-music-heading">New music in your libraries</h2></div>
            <LibraryRail items={addedMusic} label="albums" />
        </section>}
        {addedGames.length > 0 && <section className="recent" aria-labelledby="added-games-heading">
            <div className="recentHeader"><h2 id="added-games-heading">New games in your libraries</h2></div>
            <LibraryRail items={addedGames} label="games" />
        </section>}
        {addedBooks.length > 0 && <section className="recent" aria-labelledby="added-books-heading">
            <div className="recentHeader"><h2 id="added-books-heading">New audiobooks in your libraries</h2></div>
            <LibraryRail items={addedBooks} label="audiobooks" />
        </section>}
        {item && <RequestModal resolved={item} session={session} onCreated={onCreated} onClose={() => { setItem(null); void loadRecent(); if (['movie', 'tv'].includes(item.Item.MediaType)) requestAnimationFrame(() => returnFocus.current?.focus()) }} />}
    </main>
}

function SearchRow({ result, onPick }: { result: SearchResult; onPick: (opener: HTMLElement) => void }) {
    const kind = result.mediaType === 'tv' ? 'TV show' : result.mediaType === 'movie' ? 'Movie' : ''
    return <li><button type="button" className="searchRow" onClick={event => onPick(event.currentTarget)}>
        {result.artworkUrl ? <img className="searchPoster" src={result.artworkUrl} alt="" loading="lazy" /> : <span className="searchPoster" aria-hidden="true" />}
        <span className="searchText"><span className="searchTitle">{result.title}</span> <span className="searchMeta">{[result.year || '', kind, result.subtitle].filter(Boolean).join(' · ')}</span></span>
        {searchBadges(result.local).map(badge => <span key={badge.key} className={`searchBadge ${badge.key}`}><span aria-hidden="true">{badge.icon}</span> {badge.label}</span>)}
    </button></li>
}
