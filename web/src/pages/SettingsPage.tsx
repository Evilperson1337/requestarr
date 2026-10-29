import { useCallback, useEffect, useState } from 'react'
import { api, ApiError, type AdminUser, type OperationalStatus, type SecretsOverview, type Session, type SourceRuns } from '../api'
import { SecretField } from '../components/SecretField'
import { MatchSuggestions } from '../components/MatchSuggestions'
import { DevelopmentLogsPanel } from '../components/DevelopmentLogs'

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
    const [secretList, setSecretList] = useState<SecretsOverview['secrets']>([])
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
    useEffect(() => {
        if (authorizationLost) return
        const controller = new AbortController()
        api<SecretsOverview>('/admin/secrets', { cache: 'no-store', signal: controller.signal }).then(result => { if (!controller.signal.aborted && Array.isArray(result?.secrets)) setSecretList(result.secrets) })
            .catch(e => { if (!controller.signal.aborted) checkAuthorization(e) })
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
    return <main className="shell"><section className="card" aria-labelledby="opsHeading"><p className="eyebrow">Administrator</p><h1 id="opsHeading">Operational status</h1>
        {opsError && <p role="alert" className="error">{opsError}</p>}{!ops && !opsError && <p role="status">Loading operational status…</p>}
        {ops && <><h2>Library Sources</h2><div className="table-wrap" role="region" aria-label="Library Source status" tabIndex={0}><table><caption className="sr-only">Library Source status</caption><thead><tr><th scope="col">Source</th><th scope="col">Configured</th><th scope="col">Last sync</th><th scope="col">Outcome</th><th scope="col">Last success</th><th scope="col">Error</th></tr></thead><tbody>{ops.sources.map(source => <tr key={source.slug}><th scope="row">{source.name}</th><td>{source.configured ? 'Yes' : source.note ? `No (${source.note})` : 'No'}</td><td>{source.configured ? when(source.lastRun?.startedAt) : '—'}</td><td>{source.lastRun?.status ?? '—'}</td><td>{source.configured ? when(source.lastSuccessAt) : '—'}</td><td>{source.lastError || '—'}</td></tr>)}</tbody></table></div>
            <h2>Metadata and notification providers</h2><p>Configuration only. Requestarr does not call providers to render this page.</p><ul>{ops.providers.map(provider => <li key={provider.key}>{provider.name}: {provider.note ? provider.note : provider.configured ? 'Configured' : 'Not configured'}</li>)}</ul>
            <h2>Stored secrets</h2>{secretList.map(item => <SecretField key={item.name} name={item.name} label={item.label || item.name} csrfToken={session.csrfToken} onRequestError={checkAuthorization} />)}
            <h2>Recent errors</h2>{ops.recentErrors.length === 0 ? <p>No recent job errors.</p> : <ul>{ops.recentErrors.map(entry => <li key={entry.run.id}>{entry.source} · <time dateTime={entry.run.startedAt}>{when(entry.run.startedAt)}</time> · {entry.run.error || 'Failed'}</li>)}</ul>}</>}</section>
        <section className="card"><p className="eyebrow">Administrator</p><h2>Settings · Libraries</h2>{error && <p role="alert" className="error">{error}</p>}{!ops && !error && <p role="status">Loading source status…</p>}{ops?.sources.map(source => <div key={source.slug}><h3>{source.name}</h3><dl><dt>Configuration</dt><dd>{source.configured ? 'Configured' : source.note ? `Not configured (${source.note})` : 'Not configured'}</dd><dt>Last sync started</dt><dd>{source.lastRun ? when(source.lastRun.startedAt) : 'Not yet'}</dd><dt>Last successful sync</dt><dd>{source.lastSnapshotAt ? when(source.lastSnapshotAt) : 'Not yet'}</dd><dt>Last sync result</dt><dd>{source.lastError || (source.lastRun ? source.lastRun.status : 'No result recorded')}</dd></dl>{source.configured && <p><button className="button secondary" disabled={starting !== '' || syncActive} onClick={() => void syncNow(source.slug)}>{syncActive || starting === source.slug ? `Sync running…` : `Sync ${source.name} now`}</button></p>}</div>)}<p>Availability is confirmed by positive library evidence. Sync errors do not change previously observed availability.</p>{notice && <p role="status">{notice}</p>}<h4>Recent sync runs</h4><div aria-live="polite" aria-busy={syncActive}>{sources.length === 0 ? <p>No sync runs yet.</p> : sources.map(group => <div key={group.sourceId} className="table-wrap" role="region" aria-label={`${group.name || group.sourceId} sync runs`} tabIndex={0}><table><caption>{group.name || group.sourceId}</caption><thead><tr><th scope="col">Started</th><th scope="col">Trigger</th><th scope="col">Status</th><th scope="col">Finished</th><th scope="col">Error</th></tr></thead><tbody>{group.runs.map(run => <tr key={run.id}><td>{new Date(run.startedAt).toLocaleString()}</td><td>{run.trigger}</td><td>{run.status}</td><td>{run.finishedAt ? new Date(run.finishedAt).toLocaleString() : '—'}</td><td>{run.error || '—'}</td></tr>)}</tbody></table></div>)}</div></section>
        {!authorizationLost && <DevelopmentLogsPanel session={session} enabled={ops?.developmentLogsEnabled === true} onRequestError={checkAuthorization} />}
        <MatchSuggestions csrfToken={session.csrfToken} onRequestError={checkAuthorization} />
        <section className="card"><p className="eyebrow">Administrator</p><h2>User management</h2><p>Changing a role revokes that user’s active sessions. Your own role cannot be changed here.</p>{users.length === 0 ? <p role="status">Loading users…</p> : <div className="table-wrap" role="region" aria-label="Users" tabIndex={0}><table><thead><tr><th scope="col">User</th><th scope="col">Role</th><th scope="col">Action</th></tr></thead><tbody>{users.map(user => <tr key={user.id}><td>{user.displayName || user.email || user.id}<br /><small>{user.email}</small></td><td>{user.role}</td><td>{user.id === session.user.id ? 'Current user' : <label><span className="sr-only">Role for {user.displayName || user.email || user.id}</span><select value={user.role} onChange={event => void changeRole(user, event.target.value as AdminUser['role'])}><option value="viewer">viewer</option><option value="requester">requester</option><option value="admin">admin</option></select></label>}</td></tr>)}</tbody></table></div>}</section></main>
}
