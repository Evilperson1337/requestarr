import { useEffect, useState } from 'react'
import { api, type AdminUser, type JellyfinStatus, type Session } from '../api'

export function SettingsPage({ session }: { session: Session }) {
    const [status, setStatus] = useState<JellyfinStatus | null>(null)
    const [users, setUsers] = useState<AdminUser[]>([])
    const [error, setError] = useState('')
    useEffect(() => { api<JellyfinStatus>('/admin/sources/jellyfin/status').then(setStatus).catch(e => setError(e instanceof Error ? e.message : 'Could not load source status')); api<{ users: AdminUser[] }>('/admin/users').then(result => setUsers(result.users)).catch(e => setError(e instanceof Error ? e.message : 'Could not load users')) }, [])
    async function changeRole(target: AdminUser, role: AdminUser['role']) {
        setError('')
        try { await api<void>(`/admin/users/${target.id}/role`, { method: 'PUT', body: JSON.stringify({ role }) }, session.csrfToken); setUsers(current => current.map(item => item.id === target.id ? { ...item, role } : item)) }
        catch (e) { setError(e instanceof Error ? e.message : 'Could not change user role') }
    }
    return <main className="shell"><section className="card"><p className="eyebrow">Administrator</p><h1>Settings · Libraries</h1><h2>Jellyfin</h2>{error && <p role="alert" className="error">{error}</p>}{!status && !error && <p role="status">Loading source status…</p>}{status && <dl><dt>Configuration</dt><dd>{status.configured ? status.enabled ? 'Configured and enabled' : 'Configured, disabled' : 'Not configured'}</dd><dt>Last sync started</dt><dd>{status.lastSyncStartedAt ? new Date(status.lastSyncStartedAt).toLocaleString() : 'Not yet'}</dd><dt>Last successful sync</dt><dd>{status.lastSyncSucceededAt ? new Date(status.lastSyncSucceededAt).toLocaleString() : 'Not yet'}</dd><dt>Last sync result</dt><dd>{status.lastSyncError || (status.lastSyncSucceededAt ? 'Succeeded' : 'No result recorded')}</dd></dl>}<p>Availability is confirmed by positive library evidence. Sync errors do not change previously observed availability.</p></section>
        <section className="card"><p className="eyebrow">Administrator</p><h2>User management</h2><p>Changing a role revokes that user’s active sessions. Your own role cannot be changed here.</p>{users.length === 0 ? <p role="status">Loading users…</p> : <div className="table-wrap"><table><thead><tr><th scope="col">User</th><th scope="col">Role</th><th scope="col">Action</th></tr></thead><tbody>{users.map(user => <tr key={user.id}><td>{user.displayName || user.email || user.id}<br /><small>{user.email}</small></td><td>{user.role}</td><td>{user.id === session.user.id ? 'Current user' : <button className="button secondary" onClick={() => void changeRole(user, user.role === 'admin' ? 'requester' : 'admin')}>Make {user.role === 'admin' ? 'requester' : 'admin'}</button>}</td></tr>)}</tbody></table></div>}</section></main>
}
