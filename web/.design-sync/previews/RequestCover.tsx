import { RequestCover } from 'requestarr-ui'

const G = ({ children }: { children: React.ReactNode }) => <div style={{ background: 'var(--ground)', color: 'var(--text)', padding: 20 }}>{children}</div>

const poster = (title: string, from: string, to: string, w = 200, h = 300) => `data:image/svg+xml;utf8,${encodeURIComponent(`<svg xmlns="http://www.w3.org/2000/svg" width="${w}" height="${h}"><defs><linearGradient id="g" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="${from}"/><stop offset="1" stop-color="${to}"/></linearGradient></defs><rect width="100%" height="100%" fill="url(#g)"/><text x="50%" y="50%" fill="#fff" font-family="sans-serif" font-size="18" text-anchor="middle">${title}</text></svg>`)}`

const PosterInner = () => <RequestCover req={{ Title: 'Arrival', MediaType: 'movie', ArtworkURL: poster('Arrival', '#1f3a5f', '#0b1522'), ArtworkKind: 'poster' }} />
const SquareAlbumInner = () => <RequestCover req={{ Title: 'Blue Hour', MediaType: 'album', ArtworkURL: poster('Blue Hour', '#5f1f4a', '#220b1a', 300, 300), ArtworkKind: 'square' }} />
const NoArtworkTVInner = () => <RequestCover req={{ Title: 'Severance', MediaType: 'tv', ArtworkURL: '', ArtworkKind: '' }} />
const NoArtworkGameInner = () => <RequestCover req={{ Title: 'Hades', MediaType: 'game', ArtworkURL: '', ArtworkKind: '' }} />
export const Poster = () => <G><PosterInner /></G>
export const SquareAlbum = () => <G><SquareAlbumInner /></G>
export const NoArtworkTV = () => <G><NoArtworkTVInner /></G>
export const NoArtworkGame = () => <G><NoArtworkGameInner /></G>
