import type { Selection } from './api'

export const isWhole = (s?: Selection) => !s || (!s.tracks?.length && !s.seasons?.length && !s.episodes?.length)

// A short, honest summary of what a Request covers, or '' when it covers the whole item.
export function describeSelection(s?: Selection): string {
    if (isWhole(s)) return ''
    const parts: string[] = []
    if (s!.tracks?.length) {
        const shown = s!.tracks.slice(0, 3).map(t => t.title).join(', ')
        parts.push(`${s!.tracks.length} ${s!.tracks.length === 1 ? 'track' : 'tracks'}: ${shown}${s!.tracks.length > 3 ? '…' : ''}`)
    }
    if (s!.seasons?.length) parts.push(s!.seasons.map(n => (n === 0 ? 'Specials' : `Season ${n}`)).join(', '))
    if (s!.episodes?.length) {
        const shown = s!.episodes.slice(0, 4).map(e => `S${e.season}E${e.episode}`).join(', ')
        parts.push(`${shown}${s!.episodes.length > 4 ? `… (${s!.episodes.length} episodes)` : ''}`)
    }
    return parts.join(' · ')
}
