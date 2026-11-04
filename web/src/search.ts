import { useCallback, useEffect, useState } from 'react'
import { api, type SearchLocal, type SearchResult } from './api'

export type SearchState = { status: 'idle' | 'loading' | 'done' | 'error' | 'limited'; query: string; results: SearchResult[]; hasMore?: boolean; error: string }

export const searchTypeLabels: Record<string, string> = { movie_tv: 'Movies & TV', album: 'Albums', song: 'Songs', game: 'Games', audiobook: 'Audiobooks' }
// Types that search as you type; the slower providers wait for Enter.
export const typeaheadTypes = new Set(['movie_tv', 'game'])
// Slower, rate-limited providers wait for Enter or the Search button. The hint
// is read out with the input so a keyboard or screen-reader user knows why nothing appears while typing.
export const searchHints: Record<string, string> = {
    album: 'Albums are looked up when you press Enter or Search, not as you type.',
    song: 'Songs are looked up when you press Enter or Search, not as you type.',
    audiobook: 'Audiobooks are looked up when you press Enter or Search, in your administrator’s Audible marketplace. Each edition (narrator, abridged or unabridged) has its own listing and is its own request, so pick the one you want.',
}
// Types with the "include singles & EPs" switch.
export const singlesTypes = new Set(['album'])

const storageKey = 'requestarr.searchType'

// A pasted link or bare ASIN resolves as it always has; everything else is a name.
// Any "scheme://" is a link, so an unsupported one gets the resolver's explanation
// instead of a pointless title search. An Audible ASIN is "B0" and eight more
// letters or digits (an ISBN-style one is ten digits, the last possibly X), which
// keeps an ordinary ten-letter title like "Braveheart" a search.
export function isLinkInput(text: string): boolean {
    const value = text.trim()
    return /^[a-z][a-z0-9+.-]*:\/\//i.test(value) || /^www\./i.test(value) || /^B0[0-9A-Z]{8}$/i.test(value) || /^[0-9]{9}[0-9X]$/i.test(value)
}

export type SearchBadge = { key: 'requested' | 'library'; icon: string; label: string }

// What a result's local state says, as words plus an icon (never colour alone).
// Absent local state means "never resolved here" and has no badge. A Request
// that is open or approved reads "Requested"; denied, cancelled and the
// available status say nothing about a pending request, and "In library" comes
// from availability, which only an enabled library's current snapshot sets.
export function searchBadges(local?: SearchLocal): SearchBadge[] {
    const badges: SearchBadge[] = []
    if (!local) return badges
    if (local.requestStatus === 'requested') badges.push({ key: 'requested', icon: '◔', label: 'Requested' })
    else if (local.requestStatus === 'approved') badges.push({ key: 'requested', icon: '◔', label: 'Requested · Approved' })
    if (local.availability === 'available') badges.push({ key: 'library', icon: '✓', label: 'In library' })
    return badges
}

function remembered(): string {
    try { return localStorage.getItem(storageKey) ?? '' } catch { return '' }
}

// Which search types the server can serve (those whose provider is configured),
// and which one is selected. The choice is remembered on this device.
export function useSearchTypes(requested = '') {
    const [types, setTypes] = useState<string[]>([])
    const [chosen, setChosen] = useState(() => requested || remembered())
    useEffect(() => {
        const controller = new AbortController()
        api<{ types: string[] }>('/search/types', { signal: controller.signal, cache: 'no-store' })
            .then(result => { if (!controller.signal.aborted) setTypes(Array.isArray(result?.types) ? result.types.filter(type => type in searchTypeLabels) : []) })
            .catch(() => undefined)
        return () => controller.abort()
    }, [])
    const searchType = types.includes(chosen) ? chosen : (types[0] ?? '')
    useEffect(() => {
        if (searchType) try { localStorage.setItem(storageKey, searchType) } catch { /* private mode: the choice just is not remembered */ }
    }, [searchType])
    const chooseType = useCallback((type: string) => setChosen(type), [])
    return { types, searchType, chooseType }
}

// The search a Home URL carries: ?q=<text>&type=<kind>. A type that is not offered is
// ignored by useSearchTypes, so a stale bookmark still lands on a working search.
export function searchFromLocation(location: string): { q: string; type: string } {
    const params = new URLSearchParams(location.split('?')[1] ?? '')
    return { q: params.get('q') ?? '', type: params.get('type') ?? '' }
}

export function locationForSearch(q: string, type: string): string {
    return q ? `/?${new URLSearchParams({ q, type })}` : '/'
}
