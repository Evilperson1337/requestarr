import { useCallback, useEffect, useState } from 'react'
import { api, ApiError, type AdminUser, type OperationalStatus, type Session, type SourceRuns } from '../api'
import { SettingsSection } from '../components/SettingsSection'
import { MatchSuggestions } from '../components/MatchSuggestions'
import { DevelopmentLogsPanel } from '../components/DevelopmentLogs'
import { LibraryConnections } from '../components/LibraryConnections'
import { ProviderIntegrations } from '../components/ProviderIntegrations'

const tabs = [['integrations', 'Integrations'], ['activity', 'Activity'], ['matches', 'Matches'], ['users', 'Users']] as const

export function SettingsPage({ session }: { session: Session }) {
    return <SettingsForSession key={`${session.user.id}:${session.csrfToken}`} session={session} />
}

function SettingsForSession({ session }: { session: Session }) {
    const [users, setUsers] = useState<AdminUser[]>([])
    const [error, setError] = useState('')
    const [notice, setNotice] = useState('')
    const [sources, setSources] = useState<SourceRuns[]>([])
    const [starting, setStarting] = useState('')
    const [ops, setOps] = useState<OperationalStatus | null>(null)
    const [opsError, setOpsError] = useState('')
    const [authorizationLost, setAuthorizationLost] = useState(false)
    const checkAuthorization = useCallback((error: unknown) => {
        if (error instanceof ApiError && (error.status === 401 || error.status === 403)) {
            setAuthorizationLost(true)
            setOps(null)
        }
    }, [])
    useEffect(() => {
        if (authorizationLost) return
        const controller = new AbortController()
        api<OperationalStatus>('/admin/status', { cache: 'no-store', signal: controller.signal }).then(result => {
            if (!controller.signal.aborted) setOps(result)
        }).catch(e => { if (!controller.signal.aborted) { checkAuthorization(e); setOps(null); setOpsError(e instanceof Error ? e.message : 'Could not load operational status') } })
        return () => controller.abort()
    }, [session.user.id, session.csrfToken, checkAuthorization, authorizationLost])
    const loadStatus = useCallback(() => api<OperationalStatus>('/admin/status', { cache: 'no-store' }).then(setOps).catch(e => { checkAuthorization(e); setError(e instanceof Error ? e.message : 'Could not load source status') }), [checkAuthorization])
    const loadRuns = useCallback(() => api<{ sources: SourceRuns[] }>('/admin/job-runs').then(result => setSources(result.sources)).catch(e => { checkAuthorization(e); setError(e instanceof Error ? e.message : 'Could not load sync runs'); setSources(current => [...current]) /* new reference keeps an active poll retrying */ }), [checkAuthorization])
    const syncActive = sources.some(group => group.runs.some(run => run.status === 'running'))
    useEffect(() => { loadRuns() }, [loadRuns])
    // Follow an active run until it finishes.
    useEffect(() => {
        if (!syncActive || authorizationLost) return
        const timer = setTimeout(() => { void loadRuns(); void loadStatus() }, 3000)
        return () => clearTimeout(timer)
    }, [syncActive, sources, loadRuns, loadStatus, authorizationLost])
    async function syncNow(slug: string) {
        setError(''); setNotice(''); setStarting(slug)
        try { await api(`/admin/sources/${slug}/sync`, { method: 'POST' }, session.csrfToken) }
        catch (e) {
            checkAuthorization(e)
            if (e instanceof ApiError && e.code === 'sync_in_progress') setNotice('A sync is already running. Its progress is shown below.')
            else setError(e instanceof Error ? e.message : 'Could not start sync')
        } finally { setStarting(''); void loadRuns(); void loadStatus() }
    }
    useEffect(() => { api<{ users: AdminUser[] }>('/admin/users').then(result => setUsers(result.users)).catch(e => { checkAuthorization(e); setError(e instanceof Error ? e.message : 'Could not load users') }) }, [checkAuthorization])
    async function changeRole(target: AdminUser, role: AdminUser['role']) {
        setError('')
        try { await api<void>(`/admin/users/${target.id}/role`, { method: 'PUT', body: JSON.stringify({ role }) }, session.csrfToken); setUsers(current => current.map(item => item.id === target.id ? { ...item, role } : item)) }
        catch (e) { checkAuthorization(e); setError(e instanceof Error ? e.message : 'Could not change user role') }
    }
    const when = (value: string | null | undefined) => value ? new Date(value).toLocaleString() : 'Never'
    const [tab, setTab] = useState<(typeof tabs)[number][0]>('integrations')
    const failing = ops?.sources.filter(source => source.configured && source.lastRun?.status === 'failed').length ?? 0
    return <main className="shell settingsPage"><h1 id="opsHeading">Settings</h1>
        <div className="settingsTabs" role="group" aria-label="Settings sections">{tabs.map(([key, label]) => <button key={key} type="button" className="settingsTab" aria-pressed={tab === key} onClick={() => setTab(key)}>{label}{key === 'activity' && failing > 0 && <span className="tabCount" aria-label={`${failing} failing`}>{failing}</span>}</button>)}</div>
        {opsError && <p role="alert" className="error">{opsError}</p>}{!ops && !opsError && <p role="status">Loading operational status…</p>}
        {ops && <div hidden={tab !== 'integrations'}>
            <section className="integrationGroup" aria-labelledby="group-libraries"><div className="groupHead"><h2 id="group-libraries">Media libraries</h2><p>Where Requestarr looks to see what you already have. Saving an address or its credential applies at once; an address set by an environment variable is shown but cannot be changed here.</p></div>
                <LibraryConnections csrfToken={session.csrfToken} onRequestError={checkAuthorization} sources={ops.sources} syncing={starting || (syncActive ? 'running' : '')} onSync={slug => void syncNow(slug)} /></section>
            <section className="integrationGroup" aria-labelledby="group-metadata"><div className="groupHead"><h2 id="group-metadata">Metadata</h2><p>Where a pasted link is looked up. Credentials are stored encrypted, never shown again, and apply after a restart; an environment value always wins.</p></div>
                <ProviderIntegrations providers={ops.providers} keys={['tmdb', 'musicbrainz', 'igdb']} csrfToken={session.csrfToken} onRequestError={checkAuthorization} /></section>
            <section className="integrationGroup" aria-labelledby="group-notifications"><div className="groupHead"><h2 id="group-notifications">Notifications</h2><p>How people hear that a request is available.</p></div>
                <ProviderIntegrations providers={ops.providers} keys={['apprise']} csrfToken={session.csrfToken} onRequestError={checkAuthorization} /></section>
        </div>}
        <div hidden={tab !== 'activity'}>
            {ops && <SettingsSection title="Recent errors">{ops.recentErrors.length === 0 ? <p>No recent job errors.</p> : <ul>{ops.recentErrors.map(entry => <li key={entry.run.id}>{entry.source} · <time dateTime={entry.run.startedAt}>{when(entry.run.startedAt)}</time> · {entry.run.error || 'Failed'}</li>)}</ul>}</SettingsSection>}
            <SettingsSection title="Recent sync runs" description="Availability is confirmed by positive library evidence. Sync errors do not change previously observed availability.">{error && <p role="alert" className="error">{error}</p>}{notice && <p role="status">{notice}</p>}<div aria-live="polite" aria-busy={syncActive}>{sources.length === 0 ? <p>No sync runs yet.</p> : sources.map(group => <div key={group.sourceId} className="table-wrap" role="region" aria-label={`${group.name || group.sourceId} sync runs`} tabIndex={0}><table><caption>{group.name || group.sourceId}</caption><thead><tr><th scope="col">Started</th><th scope="col">Trigger</th><th scope="col">Status</th><th scope="col">Finished</th><th scope="col">Error</th></tr></thead><tbody>{group.runs.map(run => <tr key={run.id}><td>{new Date(run.startedAt).toLocaleString()}</td><td>{run.trigger}</td><td>{run.status}</td><td>{run.finishedAt ? new Date(run.finishedAt).toLocaleString() : '—'}</td><td>{run.error || '—'}</td></tr>)}</tbody></table></div>)}</div></SettingsSection>
            {!authorizationLost && <DevelopmentLogsPanel session={session} enabled={ops?.developmentLogsEnabled === true} onRequestError={checkAuthorization} />}
        </div>
        <div hidden={tab !== 'matches'}><MatchSuggestions csrfToken={session.csrfToken} onRequestError={checkAuthorization} /></div>
        <div hidden={tab !== 'users'}><SettingsSection title="User management" description="Changing a role revokes that user’s active sessions. Your own role cannot be changed here.">{users.length === 0 ? <p role="status">Loading users…</p> : <div className="table-wrap" role="region" aria-label="Users" tabIndex={0}><table><thead><tr><th scope="col">User</th><th scope="col">Role</th><th scope="col">Action</th></tr></thead><tbody>{users.map(user => <tr key={user.id}><td>{user.displayName || user.email || user.id}<br /><small>{user.email}</small></td><td>{user.role}</td><td>{user.id === session.user.id ? 'Current user' : <label><span className="sr-only">Role for {user.displayName || user.email || user.id}</span><select value={user.role} onChange={event => void changeRole(user, event.target.value as AdminUser['role'])}><option value="viewer">viewer</option><option value="requester">requester</option><option value="admin">admin</option></select></label>}</td></tr>)}</tbody></table></div>}</SettingsSection></div>
    </main>
}
