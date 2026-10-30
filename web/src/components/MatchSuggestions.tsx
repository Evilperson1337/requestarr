import { useCallback, useEffect, useState } from 'react'
import { api, ApiError, type MatchSuggestion } from '../api'

export function MatchSuggestions({ csrfToken, onRequestError }: { csrfToken: string; onRequestError?: (error: unknown) => void }) {
    const [items, setItems] = useState<MatchSuggestion[]>([])
    const [loading, setLoading] = useState(true)
    const [reviewing, setReviewing] = useState('')
    const [error, setError] = useState('')
    const [notice, setNotice] = useState('')
    const load = useCallback(async (signal?: AbortSignal) => {
        setLoading(true)
        try {
            const result = await api<{ items: MatchSuggestion[] }>('/admin/match-suggestions', { signal })
            setItems(result.items)
        } catch (e) {
            if (!signal?.aborted) { onRequestError?.(e); setError(e instanceof Error ? e.message : 'Could not load suggested matches') }
        } finally {
            if (!signal?.aborted) setLoading(false)
        }
    }, [onRequestError])
    useEffect(() => {
        const controller = new AbortController()
        void load(controller.signal)
        return () => controller.abort()
    }, [load])

    async function review(item: MatchSuggestion, decision: 'accept' | 'dismiss') {
        setReviewing(item.id); setError(''); setNotice('')
        try {
            await api<void>(`/admin/match-suggestions/${item.id}/${decision}`, { method: 'POST' }, csrfToken)
            setItems(current => current.filter(candidate => candidate.id !== item.id))
            setNotice(decision === 'accept' ? `Accepted the library match for ${item.catalogTitle}. Availability has been reconciled.` : `Dismissed the library match for ${item.catalogTitle}. Availability is unchanged.`)
            await load()
        } catch (e) {
            onRequestError?.(e)
            setError(e instanceof Error ? e.message : 'Could not review suggested match')
            if (e instanceof ApiError && (e.code === 'suggestion_stale' || e.code === 'suggestion_already_reviewed' || e.code === 'suggestion_not_found')) await load()
        } finally { setReviewing('') }
    }

    return <section className="card" aria-labelledby="match-review-heading">
        <h2 id="match-review-heading">Suggested matches</h2>
        <p>These title/year candidates are not availability evidence. Accept only when the catalog item and library item are the same. Accepting records library evidence and may notify subscribers; dismissing leaves availability unchanged and will not reopen this candidate.</p>
        <button className="button secondary" disabled={loading || !!reviewing} onClick={() => { setError(''); void load() }}>Refresh suggestions</button>
        {error && <p role="alert" className="error">{error}</p>}
        {notice && <p role="status">{notice}</p>}
        {loading && <p role="status">Loading suggested matches…</p>}
        {!loading && !error && items.length === 0 && <p>No pending suggested matches.</p>}
        <div aria-busy={loading || !!reviewing} className="table-wrap">
            {items.length > 0 && <table><caption>Up to 100 current candidates, oldest first. Refresh after reviewing to see more.</caption>
                <thead><tr><th scope="col">Catalog item</th><th scope="col">Library item</th><th scope="col">Why suggested</th><th scope="col">Review</th></tr></thead>
                <tbody>{items.map(item => <tr key={item.id}>
                    <td>{item.catalogTitle}<br /><small>{item.mediaType} · {item.catalogYear || 'Year unknown'}</small></td>
                    <td>{item.inventoryTitle}<br /><small>{item.sourceName} · {item.inventoryYear || 'Year unknown'}<br />Library ID: {item.sourceItemId}{item.platformKey && <><br />Platform: {item.platformKey}</>}<br />Observed: {new Date(item.observedAt).toLocaleString()}</small></td>
                    <td><strong>{item.confidence === 'title_year' ? 'Title and year' : 'Title only · lower confidence'}</strong><br />{item.reason}</td>
                    <td><button className="button" disabled={loading || !!reviewing} aria-label={`Accept match for ${item.catalogTitle} from ${item.sourceName}`} onClick={() => void review(item, 'accept')}>Accept match</button>{' '}<button className="button secondary" disabled={loading || !!reviewing} aria-label={`Dismiss match for ${item.catalogTitle} from ${item.sourceName}`} onClick={() => void review(item, 'dismiss')}>Dismiss</button>{reviewing === item.id && <p role="status">Saving review…</p>}</td>
                </tr>)}</tbody>
            </table>}
        </div>
    </section>
}
