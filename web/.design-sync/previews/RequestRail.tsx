import { RequestRail } from 'requestarr-ui'

const G = ({ children }: { children: React.ReactNode }) => <div style={{ background: 'var(--ground)', color: 'var(--text)', padding: 20 }}>{children}</div>

const poster = (title: string, from: string, to: string, w = 200, h = 300) => `data:image/svg+xml;utf8,${encodeURIComponent(`<svg xmlns="http://www.w3.org/2000/svg" width="${w}" height="${h}"><defs><linearGradient id="g" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="${from}"/><stop offset="1" stop-color="${to}"/></linearGradient></defs><rect width="100%" height="100%" fill="url(#g)"/><text x="50%" y="50%" fill="#fff" font-family="sans-serif" font-size="18" text-anchor="middle">${title}</text></svg>`)}`

const requests = [
  { t: 'Arrival', m: 'movie', s: 'available', c: ['#1f3a5f', '#0b1522'] },
  { t: 'Severance', m: 'tv', s: 'approved', c: ['#2f4f3a', '#0e1a12'] },
  { t: 'Blue Hour', m: 'album', s: 'requested', c: ['#5f1f4a', '#220b1a'], k: 'square' },
  { t: 'Hades', m: 'game', s: 'requested', c: ['#6a2a1f', '#220f0b'] },
  { t: 'Dune: Part Two', m: 'movie', s: 'denied', c: ['#6a5a1f', '#221d0b'] },
].map((r, i) => ({ ID: String(i), CatalogItemID: 'c' + i, Title: r.t, MediaType: r.m, ArtworkURL: poster(r.t, r.c[0], r.c[1], 200, r.k ? 200 : 300), ArtworkKind: r.k ?? 'poster', Status: r.s, Version: 1, RequestedBy: 'u1', Note: '', availability: 'unknown' as const, Platforms: { any: true }, CreatedAt: '2026-11-01T10:00:00Z', subscribed: true, subscriptionSource: 'requester' as const }))


const RequestsInner = () => <div style={{ width: 760 }}><RequestRail items={requests} /></div>
export const Requests = () => <G><RequestsInner /></G>
