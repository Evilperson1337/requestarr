import type { RequestItem } from '../api'

// A request's cover: its artwork when it has any, else a plain tile naming the media type.
export function RequestCover({ req }: { req: Pick<RequestItem, 'Title' | 'MediaType' | 'ArtworkURL' | 'ArtworkKind'> }) {
    if (req.ArtworkURL) return <img src={req.ArtworkURL} alt={`Artwork for ${req.Title}`} className={`art small${req.ArtworkKind === 'square' ? ' square' : ''}`} />
    return <span className="artPlaceholder" aria-hidden="true">{req.MediaType === 'tv' ? 'TV' : req.MediaType}</span>
}
