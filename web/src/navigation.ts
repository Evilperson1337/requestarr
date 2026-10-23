import { useSyncExternalStore, type MouseEvent } from 'react'

function subscribe(listener: () => void) {
    window.addEventListener('popstate', listener)
    window.addEventListener('requestarr:navigate', listener)
    return () => {
        window.removeEventListener('popstate', listener)
        window.removeEventListener('requestarr:navigate', listener)
    }
}

export function useLocation() {
    return useSyncExternalStore(subscribe, () => location.pathname + location.search)
}

export function navigate(href: string, state: unknown = {}, replace = false) {
    if (replace) history.replaceState(state, '', href)
    else history.pushState(state, '', href)
    window.dispatchEvent(new Event('requestarr:navigate'))
}

// Keep native links working for new tabs, modifier keys, and copied URLs.
export function followLink(event: MouseEvent<HTMLAnchorElement>, state: unknown = {}) {
    if (!isPlainClick(event)) return
    event.preventDefault()
    navigate(event.currentTarget.href, state)
}

export function followBackLink(event: MouseEvent<HTMLAnchorElement>) {
    if (!isPlainClick(event)) return
    event.preventDefault()
    history.back()
}

function isPlainClick(event: MouseEvent<HTMLAnchorElement>) {
    return !event.defaultPrevented && event.button === 0 && !event.metaKey && !event.ctrlKey && !event.shiftKey && !event.altKey
}
