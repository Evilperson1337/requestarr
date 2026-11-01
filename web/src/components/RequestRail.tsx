import type { RequestItem } from '../api'
import { followLink } from '../navigation'

const kinds: Record<string, string> = { movie: 'Movie', tv: 'TV', game: 'Game', album: 'Album', song: 'Song' }

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
