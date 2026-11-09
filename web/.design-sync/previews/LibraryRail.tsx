import { LibraryRail } from 'requestarr-ui'

const G = ({ children }: { children: React.ReactNode }) => <div style={{ background: 'var(--ground)', color: 'var(--text)', padding: 20 }}>{children}</div>

const poster = (title: string, from: string, to: string, w = 200, h = 300) => `data:image/svg+xml;utf8,${encodeURIComponent(`<svg xmlns="http://www.w3.org/2000/svg" width="${w}" height="${h}"><defs><linearGradient id="g" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="${from}"/><stop offset="1" stop-color="${to}"/></linearGradient></defs><rect width="100%" height="100%" fill="url(#g)"/><text x="50%" y="50%" fill="#fff" font-family="sans-serif" font-size="18" text-anchor="middle">${title}</text></svg>`)}`

const library = ['Heat', 'Paddington 2', 'Aphex Twin: Drukqs', 'Celeste', 'The Overstory', 'Alien', 'Stalker', 'Hollow Knight'].map((t, i) => ({ id: 'l' + i, title: t, year: 1990 + i * 4, mediaType: (['movie', 'movie', 'album', 'game', 'audiobook', 'movie', 'movie', 'game'] as const)[i], source: ['Jellyfin', 'Plex', 'Navidrome', 'RomM'][i % 4], addedAt: '2026-11-0' + (i + 1), artworkUrl: i === 3 ? undefined : poster(t, ['#1f3a5f', '#5f1f4a', '#2f4f3a', '#6a2a1f'][i % 4], '#0b0b0b', 200, [2, 4].includes(i) ? 200 : 300) }))

const RecentlyAddedInner = () => <div style={{ width: 760 }}><LibraryRail items={library} label="additions" onOpen={() => {}} /></div>
const FewItemsInner = () => <div style={{ width: 760 }}><LibraryRail items={library.slice(0, 2)} label="additions" onOpen={() => {}} /></div>
export const RecentlyAdded = () => <G><RecentlyAddedInner /></G>
export const FewItems = () => <G><FewItemsInner /></G>
