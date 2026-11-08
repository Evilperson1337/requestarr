import { useCallback, useEffect, useRef, useState } from 'react'
import { api, type ServerLogEntry, type ServerLogs as ServerLogsResult } from '../api'
import { SettingsSection } from './SettingsSection'

const levels = [['debug', 'Everything'], ['info', 'Info and above'], ['warn', 'Warnings and errors'], ['error', 'Errors only']] as const
type Level = (typeof levels)[number][0]
const levelClass: Record<string, string> = { ERROR: 'logLevel logError', WARN: 'logLevel logWarn', INFO: 'logLevel', DEBUG: 'logLevel logDebug' }
const followEvery = 5000

// Settings, Logs: the server's recent log records, newest first (D042). It reads
// what the server already wrote to standard output; nothing is stored here.
export function ServerLogs({ onRequestError }: { onRequestError: (error: unknown) => void }) {
    const [level, setLevel] = useState<Level>('info')
    const [entries, setEntries] = useState<ServerLogEntry[]>([])
    const [capacity, setCapacity] = useState(0)
    const [filter, setFilter] = useState('')
    const [follow, setFollow] = useState(false)
    const [loading, setLoading] = useState(false)
    const [loaded, setLoaded] = useState(false)
    const [error, setError] = useState('')
    const last = useRef(0)
    const active = useRef<AbortController | null>(null)

    // A full read replaces the list; a follow-up read asks only for what is new.
    const load = useCallback(async (more: boolean) => {
        active.current?.abort()
        const controller = new AbortController()
        active.current = controller
        if (!more) setLoading(true)
        setError('')
        try {
            const after = more ? `&after=${last.current}` : ''
            const result = await api<ServerLogsResult>(`/admin/logs?level=${level}${after}`, { cache: 'no-store', signal: controller.signal })
            if (controller.signal.aborted) return
            const fresh = result.entries ?? []
            if (fresh.length > 0) last.current = fresh[fresh.length - 1].seq
            else if (!more) last.current = 0
            setCapacity(result.capacity)
            setEntries(current => (more ? [...current, ...fresh] : fresh).slice(-result.capacity))
            setLoaded(true)
        } catch (e) {
            if (controller.signal.aborted) return
            onRequestError(e)
            setError(e instanceof Error ? e.message : 'Could not load the server logs')
            setFollow(false)
        } finally {
            if (!controller.signal.aborted) setLoading(false)
        }
    }, [level, onRequestError])

    useEffect(() => { void load(false); return () => active.current?.abort() }, [load])
    useEffect(() => {
        if (!follow) return
        const timer = setInterval(() => void load(true), followEvery)
        return () => clearInterval(timer)
    }, [follow, load])

    const needle = filter.trim().toLowerCase()
    const shown = entries.filter(entry => !needle || entry.message.toLowerCase().includes(needle) || entry.attrs.some(a => `${a.key}=${a.value}`.toLowerCase().includes(needle))).reverse()

    return <SettingsSection title="Server logs" description={<>The most recent {capacity || 1000} records the server wrote, newest first, kept in memory only: a restart clears them. They are the same lines the container writes to its standard output, at the level set by <code>REQUESTARR_LOG_LEVEL</code> (set it to <code>debug</code> for more detail). Credentials, group names and search text are never logged.</>}>
        <div className="logControls">
            <label className="field">Show<select value={level} onChange={event => setLevel(event.target.value as Level)}>{levels.map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select></label>
            <label className="field">Filter<input type="search" value={filter} onChange={event => setFilter(event.target.value)} placeholder="e.g. romm, sign-in, failed" /></label>
            <label className="logFollow"><input type="checkbox" checked={follow} onChange={event => setFollow(event.target.checked)} />Follow new records</label>
            <button type="button" className="button secondary" disabled={loading} onClick={() => void load(false)}>Refresh</button>
        </div>
        <p role="status" aria-live="polite">{loading ? 'Loading server logs…' : loaded ? `${shown.length} of ${entries.length} records shown.` : ''}</p>
        {error && <p role="alert" className="error">{error}</p>}
        {loaded && entries.length === 0 && <p>No records at this level yet.</p>}
        {shown.length > 0 && <div className="table-wrap logTable" role="region" aria-label="Server log records" tabIndex={0}><table>
            <thead><tr><th scope="col">Time</th><th scope="col">Level</th><th scope="col">Message</th><th scope="col">Details</th></tr></thead>
            <tbody>{shown.map(entry => <tr key={entry.seq}>
                <td><time dateTime={entry.time}>{new Date(entry.time).toLocaleString()}</time></td>
                <td><span className={levelClass[entry.level] ?? 'logLevel'}>{entry.level}</span></td>
                <td>{entry.message}</td>
                <td>{entry.attrs.length === 0 ? '—' : <ul className="logAttrs">{entry.attrs.map((a, i) => <li key={i}><span className="logKey">{a.key}</span>={a.value}</li>)}</ul>}</td>
            </tr>)}</tbody>
        </table></div>}
    </SettingsSection>
}
