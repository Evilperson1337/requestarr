import { useEffect, useId, useState } from 'react'

// Parts the two Authentication cards share: loading a resource with retry, the placeholder shown
// until it arrives, and the "one request at a time" wrapper with its error and notice.

// useLoadedResource loads once, and again each time retry() is called. load must be a stable
// function. A load that is superseded or unmounted is aborted and its result ignored.
export function useLoadedResource<T>(load: (signal: AbortSignal) => Promise<T>, onRequestError: (error: unknown) => void, failure: string, onLoaded?: (value: T) => void) {
    const [value, setValue] = useState<T | null>(null)
    const [loadError, setLoadError] = useState('')
    const [attempt, setAttempt] = useState(0)
    useEffect(() => {
        const controller = new AbortController()
        setLoadError('')
        load(controller.signal).then(result => { if (!controller.signal.aborted) { setValue(result); onLoaded?.(result) } })
            .catch(e => { if (!controller.signal.aborted) { onRequestError(e); setLoadError(failure) } })
        return () => controller.abort()
    }, [load, onLoaded, onRequestError, failure, attempt])
    return { value, setValue, loadError, retry: () => setAttempt(n => n + 1) }
}

export function LoadingCard({ title, error, onRetry }: { title: string; error: string; onRetry: () => void }) {
    const id = useId()
    return <li className="integration" aria-labelledby={`${id}-load`}>
        <h3 id={`${id}-load`} className="authLoadingTitle">{title}</h3>
        {error ? <div role="alert" className="errorBox"><p>{error}</p><button type="button" className="button secondary" onClick={onRetry}>Try again</button></div> : <p role="status">Loading…</p>}
    </li>
}

// useBusy runs one request at a time, named by kind (a union of the card's own actions). A failure
// goes to the card's error line; explain may add to its text.
export function useBusy<Kind extends string>(onRequestError: (error: unknown) => void) {
    const [busy, setBusy] = useState<Kind | ''>('')
    const [error, setError] = useState('')
    const [notice, setNotice] = useState('')
    async function run(kind: Kind, work: () => Promise<void>, explain: (message: string) => string = message => message) {
        if (busy) return
        setBusy(kind); setError(''); setNotice('')
        try { await work() } catch (e) {
            onRequestError(e)
            setError(explain(e instanceof Error && e.message ? e.message : 'The request failed.'))
        } finally { setBusy('') }
    }
    return { busy, setBusy, error, setError, notice, setNotice, run }
}
