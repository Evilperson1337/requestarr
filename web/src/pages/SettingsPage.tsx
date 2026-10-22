import { useCallback, useEffect, useState } from 'react'
import { api, ApiError, type AdminUser, type JellyfinStatus, type Session, type SourceRuns } from '../api'
import { MatchSuggestions } from '../components/MatchSuggestions'

export function SettingsPage({ session }: { session: Session }) {
    const [status, setStatus] = useState<JellyfinStatus | null>(null)
    const [users, setUsers] = useState<AdminUser[]>([])
    const [error, setError] = useState('')
    const [notice, setNotice] = useState('')
    const [sources, setSources] = useState<SourceRuns[]>([])
    const [starting, setStarting] = useState(false)
    const loadStatus = useCallback(() => api<JellyfinStatus>('/admin/sources/jellyfin/status').then(setStatus).catch(e => setError(e instanceof Error ? e.message : 'Could not load source status')), [])
    const loadRuns = useCallback(() => api<{ sources: SourceRuns[] }>('/admin/job-runs').then(result => setSources(result.sources)).catch(e => { setError(e instanceof Error ? e.message : 'Could not load sync runs'); setSources(current => [...current]) /* new reference keeps an active poll retrying */ }), [])
    const syncActive = sources.some(group => group.runs.some(run => run.status === 'running'))
    useEffect(() => { loadRuns() }, [loadRuns])
    // Follow an active run until it finishes.
    useEffect(() => {
        if (!syncActive) return
        const timer = setTimeout(() => { void loadRuns(); void loadStatus() }, 3000)
        return () => clearTimeout(timer)
    }, [syncActive, sources, loadRuns, loadStatus])
    async function syncNow() {
        setError(''); setNotice(''); setStarting(true)
        try { await api('/admin/sources/jellyfin/sync', { method: 'POST' }, session.csrfToken) }
        catch (e) {
            if (e instanceof ApiError && e.code === 'sync_in_progress') setNotice('A sync is already running. Its progress is shown below.')
            else setError(e instanceof Error ? e.message : 'Could not start sync')
        } finally { setStarting(false); void loadRuns() }
    }
    useEffect(() => { loadStatus(); api<{ users: AdminUser[] }>('/admin/users').then(result => setUsers(result.users)).catch(e => setError(e instanceof Error ? e.message : 'Could not load users')) }, [])
    async function changeRole(target: AdminUser, role: AdminUser['role']) {
        setError('')
        try { await api<void>(`/admin/users/${target.id}/role`, { method: 'PUT', body: JSON.stringify({ role }) }, session.csrfToken); setUsers(current => current.map(item => item.id === target.id ? { ...item, role } : item)) }
        catch (e) { setError(e instanceof Error ? e.message : 'Could not change user role') }
    }
    return <main className="shell"><section className="card"><p className="eyebrow">Administrator</p><h1>Settings · Libraries</h1><h2>Jellyfin</h2>{error && <p role="alert" className="error">{error}</p>}{!status && !error && <p role="status">Loading source status…</p>}{status && <dl><dt>Configuration</dt><dd>{status.configured ? status.enabled ? 'Configured and enabled' : 'Configured, disabled' : 'Not configured'}</dd><dt>Last sync started</dt><dd>{status.lastSyncStartedAt ? new Date(status.lastSyncStartedAt).toLocaleString() : 'Not yet'}</dd><dt>Last successful sync</dt><dd>{status.lastSyncSucceededAt ? new Date(status.lastSyncSucceededAt).toLocaleString() : 'Not yet'}</dd><dt>Last sync result</dt><dd>{status.lastSyncError || (status.lastSyncSucceededAt ? 'Succeeded' : 'No result recorded')}</dd></dl>}<p>Availability is confirmed by positive library evidence. Sync errors do not change previously observed availability.</p>{status?.configured && <p><button className="button secondary" disabled={starting || syncActive} onClick={() => void syncNow()}>{syncActive ? 'Sync running…' : 'Sync now'}</button></p>}{notice && <p role="status">{notice}</p>}<h3>Recent sync runs</h3><div aria-live="polite" aria-busy={syncActive}>{sources.length === 0 ? <p>No sync runs yet.</p> : sources.map(group => <div key={group.sourceId} className="table-wrap"><table><caption>{group.name || group.sourceId}</caption><thead><tr><th scope="col">Started</th><th scope="col">Trigger</th><th scope="col">Status</th><th scope="col">Finished</th><th scope="col">Error</th></tr></thead><tbody>{group.runs.map(run => <tr key={run.id}><td>{new Date(run.startedAt).toLocaleString()}</td><td>{run.trigger}</td><td>{run.status}</td><td>{run.finishedAt ? new Date(run.finishedAt).toLocaleString() : '—'}</td><td>{run.error || '—'}</td></tr>)}</tbody></table></div>)}</div></section>
        <MatchSuggestions csrfToken={session.csrfToken} />
        <section className="card"><p className="eyebrow">Administrator</p><h2>User management</h2><p>Changing a role revokes that user’s active sessions. Your own role cannot be changed here.</p>{users.length === 0 ? <p role="status">Loading users…</p> : <div className="table-wrap"><table><thead><tr><th scope="col">User</th><th scope="col">Role</th><th scope="col">Action</th></tr></thead><tbody>{users.map(user => <tr key={user.id}><td>{user.displayName || user.email || user.id}<br /><small>{user.email}</small></td><td>{user.role}</td><td>{user.id === session.user.id ? 'Current user' : <label><span className="sr-only">Role for {user.displayName || user.email || user.id}</span><select value={user.role} onChange={event => void changeRole(user, event.target.value as AdminUser['role'])}><option value="viewer">viewer</option><option value="requester">requester</option><option value="admin">admin</option></select></label>}</td></tr>)}</tbody></table></div>}</section></main>
}
