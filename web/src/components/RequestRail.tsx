import { useRef, useState, type TouchEvent } from 'react'
import type { RecentLibraryItem, RequestItem } from '../api'
import { followLink } from '../navigation'
import { useRailColumns } from './useRailColumns'

const kinds: Record<string, string> = { movie: 'Movie', tv: 'TV', game: 'Game', album: 'Album', song: 'Song', audiobook: 'Audiobook' }

// A row of request covers: artwork, the status on its corner, the media type on the other, then title.
export function RequestRail({ items }: { items: RequestItem[] }) {
    const { ref, columns } = useRailColumns<HTMLUListElement>()
    return <ul ref={ref} className="coverRail" style={{ gridTemplateColumns: `repeat(${columns}, minmax(0, 1fr))` }}>{items.slice(0, columns).map(req => <li key={req.ID}>
        <a className="railCard" href={`/requests?requestId=${encodeURIComponent(req.ID)}`} onClick={followLink}>
            <span className={`railCover${req.ArtworkKind === 'square' ? ' square' : ''}`}>
                {req.ArtworkURL ? <img src={req.ArtworkURL} alt={`Artwork for ${req.Title}`} loading="lazy" /> : <span className="railBlank" aria-hidden="true">{kinds[req.MediaType] ?? req.MediaType}</span>}
                <span className={`railStatus status-${req.Status}`}>{req.Status}</span>
                <span className="railOpen" aria-hidden="true">View request</span>
            </span>
            <span className="railTitle">{req.Title}</span>
            <span className="railKind">{kinds[req.MediaType] ?? req.MediaType}</span>
        </a>
    </li>)}</ul>
}

// What the libraries recently gained: covers from the library itself, each opening that item as in the Library.
export function LibraryRail({ items, label, onOpen }: { items: RecentLibraryItem[]; label: string; onOpen: (item: RecentLibraryItem) => void }) {
    const { ref, columns } = useRailColumns<HTMLUListElement>()
    const [requested, setPage] = useState(0)
    const pages = Math.max(1, Math.ceil(items.length / columns))
    const page = Math.min(requested, pages - 1)
    // A horizontal swipe pages like the arrows: left for older, right for newer. Vertical drags still scroll the page.
    const touch = useRef<{ x: number; y: number } | null>(null)
    const go = (to: number) => setPage(Math.max(0, Math.min(pages - 1, to)))
    const swipeStart = (e: TouchEvent) => { touch.current = { x: e.touches[0].clientX, y: e.touches[0].clientY } }
    const swipeEnd = (e: TouchEvent) => {
        const start = touch.current; touch.current = null
        if (!start || pages < 2) return
        const dx = e.changedTouches[0].clientX - start.x, dy = e.changedTouches[0].clientY - start.y
        if (Math.abs(dx) >= 50 && Math.abs(dx) > Math.abs(dy) * 1.5) go(dx < 0 ? page + 1 : page - 1)
    }
    return <div className="railWrap" onTouchStart={swipeStart} onTouchEnd={swipeEnd} onTouchCancel={() => { touch.current = null }}>
        {pages > 1 && <button type="button" className="railArrow prev" aria-label={`Newer ${label}`} disabled={page === 0} onClick={() => setPage(page - 1)}>‹</button>}
        <ul ref={ref} className="coverRail" style={{ gridTemplateColumns: `repeat(${columns}, minmax(0, 1fr))` }}>{items.slice(page * columns, (page + 1) * columns).map(item => <li key={item.id}>
            <button type="button" className="railCard libraryCard" onClick={() => onOpen(item)}>
                <span className={`railCover${item.mediaType === 'album' || item.mediaType === 'audiobook' ? ' square' : item.mediaType === 'game' ? ' game' : ''}`}>
                    {item.artworkUrl ? <img src={item.artworkUrl} alt={`Cover for ${item.title}`} loading="lazy" /> : <span className="railBlank" aria-hidden="true">{kinds[item.mediaType] ?? item.mediaType}</span>}
                </span>
                <span className="railTitle">{item.title}</span>
                <span className="railKind">{[kinds[item.mediaType] ?? item.mediaType, item.year || '', item.detail || '', item.source].filter(Boolean).join(' · ')}</span>
            </button>
        </li>)}</ul>
        {pages > 1 && <button type="button" className="railArrow next" aria-label={`Older ${label}`} disabled={page === pages - 1} onClick={() => setPage(page + 1)}>›</button>}
    </div>
}
