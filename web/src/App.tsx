import { useEffect, useState } from 'react'
import { api, type Session, type SetupStatus } from './api'
import { HomePage } from './pages/HomePage'
import { RequestsPage } from './pages/RequestsPage'
import { NotificationsPage } from './pages/NotificationsPage'
import { SettingsPage } from './pages/SettingsPage'
import { SetupWizardPage } from './pages/SetupWizardPage'
import { SetupDraftPage } from './pages/SetupDraftPage'
import { followLink, navigate, useLocation } from './navigation'

export function App() {
    const [session, setSession] = useState<Session | null>(null)
    const [checking, setChecking] = useState(true)
    const [setupStatus, setSetupStatus] = useState<SetupStatus | null>(null)
    const path = useLocation().split('?')[0].replace(/\/$/, '')
    const page = path === '/setup' ? 'setup' : path === '/requests' ? 'requests' : path === '/notifications' ? 'notifications' : path === '/settings' ? 'settings' : 'home'
    const [error, setError] = useState('')
    const refresh = async () => {
        setChecking(true)
        try {
            const [status, current] = await Promise.all([
                api<SetupStatus>('/setup/status', { cache: 'no-store' }),
                api<Session>('/auth/me', { cache: 'no-store' }).catch(() => null),
            ])
            setSetupStatus(status)
            setSession(current)
        } catch {
            setError('Could not check first-run setup status. Refresh the page to try again.')
        } finally { setChecking(false) }
    }
    useEffect(() => { void refresh() }, [])
    async function logout() {
        try { await api<void>('/auth/logout', { method: 'POST' }, session?.csrfToken); setSession(null) }
        catch (e) { setError(e instanceof Error ? e.message : 'Could not sign out') }
    }
    if (checking || !setupStatus) return <main className="shell" aria-busy="true"><p role="status">{error || 'Checking setup and your session…'}</p></main>
    if (setupStatus.required) return <SetupWizardPage session={session} onComplete={current => { setSession(current); setSetupStatus({ complete: true, required: false }); navigate('/setup') }} />
    if (!session) return <main className="shell"><section className="card"><h1>Sign in to Requestarr</h1><p>Sign in with your organization account to request and track availability.</p>{error && <p role="alert" className="error">{error}</p>}<a className="button" href="/api/v1/auth/login">Sign in</a></section></main>
    return <><header className="topbar"><a className="brand" href="/" onClick={followLink}>Requestarr</a><nav aria-label="Main navigation"><a className="navlink" href="/" onClick={followLink} aria-current={page === 'home' ? 'page' : undefined}>Request</a><a className="navlink" href="/requests" onClick={followLink} aria-current={page === 'requests' ? 'page' : undefined}>Requests</a><a className="navlink" href="/notifications" onClick={followLink} aria-current={page === 'notifications' ? 'page' : undefined}>Notifications</a>{session.user.role === 'admin' && <>{!setupStatus.wizardComplete && <a className="navlink" href="/setup" onClick={followLink} aria-current={page === 'setup' ? 'page' : undefined}>Setup</a>}<a className="navlink" href="/settings" onClick={followLink} aria-current={page === 'settings' ? 'page' : undefined}>Settings</a></>}</nav><div className="account"><span>{session.user.displayName || session.user.email}</span><button className="button secondary" onClick={() => void logout()}>Sign out</button></div></header>{error && <p role="alert" className="shell error">{error}</p>}{page === 'setup' ? session.user.role === 'admin' ? <SetupDraftPage session={session} onFinished={() => setSetupStatus(current => current && ({ ...current, wizardComplete: true }))} /> : <main className="shell"><section className="card"><h1>Administrator access required</h1><p>Only administrators can view or edit the shared setup draft.</p></section></main> : page === 'home' ? <HomePage session={session} onCreated={() => navigate('/requests')} /> : page === 'requests' ? <RequestsPage user={session.user} csrfToken={session.csrfToken} /> : page === 'settings' && session.user.role === 'admin' ? <SettingsPage session={session} /> : <NotificationsPage session={session} />}</>
}
