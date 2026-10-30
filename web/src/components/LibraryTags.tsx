import type { MoviePreviewState } from '../api'

// Where a catalog item is available: one tag per media server that has it,
// linked to the item there, or a single tag when no server is known to.
export function LibraryTags({ preview }: { preview: MoviePreviewState }) {
    return <ul className="tags" aria-label="Library availability">{preview.availability === 'available' && preview.libraries?.length
        ? preview.libraries.map(link => <li key={link.url}><a className="tag tag-available" href={link.url} target="_blank" rel="noreferrer">{link.name}: Available</a></li>)
        : <li className={`tag${preview.availability === 'available' ? ' tag-available' : ''}`}>{preview.availability === 'available' ? 'Available' : preview.availability === 'missing' ? 'Not in your libraries' : 'Availability unknown'}</li>}</ul>
}
