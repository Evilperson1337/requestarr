import type { RecentLibraryItem, RequestItem } from '../api'
import { followLink } from '../navigation'

const kinds: Record<string, string> = { movie: 'Movie', tv: 'TV', game: 'Game', album: 'Album', song: 'Song', audiobook: 'Audiobook' }

// A row of request covers: artwork, the status on its corner, the media type on the other, then title.
export function RequestRail({ items }: { items: RequestItem[] }) {
    return <ul className="coverRail">{items.map(req => <li key={req.ID}>
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

// What the libraries recently gained: covers from the library itself, no status, nothing to open.
export function LibraryRail({ items }: { items: RecentLibraryItem[] }) {
    return <ul className="coverRail">{items.map(item => <li key={item.id}>
        <div className="railCard plain">
            <span className={`railCover${item.mediaType === 'album' || item.mediaType === 'audiobook' ? ' square' : item.mediaType === 'game' ? ' game' : ''}`}>
                {item.artworkUrl ? <img src={item.artworkUrl} alt={`Cover for ${item.title}`} loading="lazy" /> : <span className="railBlank" aria-hidden="true">{kinds[item.mediaType] ?? item.mediaType}</span>}
            </span>
            <span className="railTitle">{item.title}</span>
            <span className="railKind">{[kinds[item.mediaType] ?? item.mediaType, item.year || '', item.detail || '', item.source].filter(Boolean).join(' · ')}</span>
        </div>
    </li>)}</ul>
}
