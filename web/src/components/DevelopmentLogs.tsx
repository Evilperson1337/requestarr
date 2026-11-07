import { useEffect, useRef, useState } from 'react'
import { ApiError, type DevelopmentLogs, type OperationalStatus, type Session } from '../api'

// This endpoint deliberately never surfaces a response/error body on failure.
// Only the fixed structured success contract belongs in this panel.
async function readDiagnostics<T>(path: string, signal: AbortSignal): Promise<T> {
    const response = await fetch(`/api/v1/admin/${path}`, { credentials: 'same-origin', cache: 'no-store', signal })
    if (!response.ok) throw new ApiError('Could not read development diagnostics.', '', response.status)
    return response.json() as Promise<T>
}

function unauthorized(error: unknown) {
    return error instanceof ApiError && (error.status === 401 || error.status === 403)
}

// Settings supplies its existing status capability. Setup omits it and performs
// one optional, local-only status read, independent of loading/checking the draft.
export function DevelopmentLogsPanel({ session, enabled, onRequestError }: { session: Session; enabled?: boolean; onRequestError?: (error: unknown) => void }) {
    if (!session.capabilities.manageSystem) return null
    return <DevelopmentLogsForAdmin key={`${session.user.id}:${session.csrfToken}`} enabled={enabled} onRequestError={onRequestError} />
}

function DevelopmentLogsForAdmin({ enabled, onRequestError }: { enabled?: boolean; onRequestError?: (error: unknown) => void }) {
    const [capability, setCapability] = useState(false)
    const [denied, setDenied] = useState(false)
    const [open, setOpen] = useState(false)
    const [entries, setEntries] = useState<DevelopmentLogs['entries']>([])
    const [loading, setLoading] = useState(false)
    const [error, setError] = useState('')
    const [loaded, setLoaded] = useState(false)
    const active = useRef<AbortController | null>(null)
    const available = !denied && (enabled ?? capability)

    useEffect(() => {
        if (enabled !== undefined || denied) return
        const controller = new AbortController()
        void readDiagnostics<OperationalStatus>('status', controller.signal).then(status => {
            if (!controller.signal.aborted) setCapability(status.developmentLogsEnabled === true)
        }).catch(e => {
            if (!controller.signal.aborted && unauthorized(e)) { setDenied(true); onRequestError?.(e) }
            // Optional capability: failed/unavailable status leaves the panel hidden.
        })
        return () => controller.abort()
    }, [enabled, denied, onRequestError])

    function clear() {
        active.current?.abort(); active.current = null
        setEntries([]); setLoading(false); setLoaded(false); setError('')
    }
    useEffect(() => {
        if (!available) { clear(); setOpen(false) }
        return () => { active.current?.abort(); active.current = null }
    }, [available])

    async function refresh() {
        if (!available) return
        active.current?.abort()
        const controller = new AbortController()
        active.current = controller
        setLoading(true); setError('')
        try {
            const result = await readDiagnostics<DevelopmentLogs>('development-logs', controller.signal)
            if (controller.signal.aborted || active.current !== controller) return
            setEntries(result.entries.slice(-200)); setLoaded(true)
        } catch (e) {
            if (controller.signal.aborted || active.current !== controller) return
            setEntries([]); setLoaded(false)
            if (unauthorized(e)) { setDenied(true); clear(); setOpen(false); onRequestError?.(e) }
            else setError('Could not load development connection logs. Try Refresh logs again.')
        } finally {
            if (!controller.signal.aborted && active.current === controller) setLoading(false)
        }
    }

    if (!available) return null
    return <details className="card developmentLogs" open={open} onToggle={event => {
        const next = event.currentTarget.open
        if (next === open) return
        setOpen(next)
        if (next) void refresh()
        else clear()
    }}>
        <summary>Development connection logs</summary>
        {open && <>
            <p>Development-only connection diagnostics, not server or Docker logs. At most 200 records, oldest first. Restarting Requestarr clears this history. Records do not mean a runtime integration has been activated.</p>
            <p>Re-run a setup connection check to record new diagnostics, then refresh here. Reading this panel does not call providers. Nothing is saved in browser storage.</p>
            <button className="button secondary" disabled={loading} onClick={() => void refresh()}>Refresh logs</button>
            <div role="status" aria-live="polite">{loading ? 'Loading development connection logs…' : loaded ? `${entries.length} connection records.` : ''}</div>
            {error && <p role="alert" className="error">{error}</p>}
            {loaded && entries.length === 0 && <p>No connection diagnostics recorded yet. Run a setup connection check, then refresh.</p>}
            {entries.length > 0 && <div className="table-wrap" role="region" aria-label="Development connection records" tabIndex={0}><table>
                <caption>Recent connection checks (oldest first)</caption>
                <thead><tr>{['Sequence', 'Time', 'Target', 'Stage', 'Outcome', 'Code', 'HTTP status', 'Duration'].map(label => <th scope="col" key={label}>{label}</th>)}</tr></thead>
                <tbody>{entries.map(entry => <tr key={entry.seq}>
                    <td>{entry.seq}</td><td><time dateTime={entry.time}>{new Date(entry.time).toLocaleString()}</time></td>
                    <td>{entry.target}</td><td>{entry.stage}</td><td>{entry.outcome}</td><td>{entry.code}</td>
                    <td>{entry.httpStatus ?? '—'}</td><td>{entry.durationMs} ms</td>
                </tr>)}</tbody>
            </table></div>}
        </>}
    </details>
}
