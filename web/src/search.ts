import { useCallback, useEffect, useState } from 'react'
import { api, type SearchResult } from './api'

export type SearchState = { status: 'idle' | 'loading' | 'done' | 'error' | 'limited'; query: string; results: SearchResult[]; hasMore?: boolean; error: string }

export const searchTypeLabels: Record<string, string> = { movie_tv: 'Movies & TV', album: 'Albums', song: 'Songs', game: 'Games', audiobook: 'Audiobooks' }
// Types that search as you type; the slower providers wait for Enter.
export const typeaheadTypes = new Set(['movie_tv', 'game'])

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
