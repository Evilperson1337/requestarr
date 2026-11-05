import { useEffect, useState } from 'react'
import { api, type Session, type SetupStatus } from './api'
import { HomePage } from './pages/HomePage'
import { RequestsPage } from './pages/RequestsPage'
import { NotificationsPage } from './pages/NotificationsPage'
import { LibraryPage } from './pages/LibraryPage'
import { IssuesPage } from './pages/IssuesPage'
import { SettingsPage } from './pages/SettingsPage'
import { SetupWizardPage } from './pages/SetupWizardPage'
import { GetStartedPage } from './pages/GetStartedPage'
import tmdbLogo from './assets/tmdb-attribution.svg'
import { followLink, navigate, useLocation } from './navigation'
import { BrandMark } from './components/BrandMark'

export function App() {
    const [session, setSession] = useState<Session | null>(null)
    const [checking, setChecking] = useState(true)
    const [setupStatus, setSetupStatus] = useState<SetupStatus | null>(null)
    const path = useLocation().split('?')[0].replace(/\/$/, '')
    const page = path === '/get-started' ? 'get-started' : path === '/library' ? 'library' : path === '/issues' ? 'issues' : path === '/requests' ? 'requests' : path === '/notifications' ? 'notifications' : path === '/settings' ? 'settings' : 'home'
    const [error, setError] = useState('')
    const [openIssues, setOpenIssues] = useState(0)
    // The Get started checklist is offered to administrators until it is done or dismissed (D038).
    const [checklistVisible, setChecklistVisible] = useState(false)
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
    // Only administrators have a queue to work through, so only they get a count (D030).
    const countIssues = () => { if (session?.user.role === 'admin') api<{ open: number }>('/issues/summary', { cache: 'no-store' }).then(result => setOpenIssues(result.open)).catch(() => undefined) }
    useEffect(() => { countIssues() }, [session?.user.id, page])
    useEffect(() => {
        setChecklistVisible(false)
        if (session?.user.role === 'admin') api<{ visible: boolean }>('/admin/checklist', { cache: 'no-store' }).then(result => setChecklistVisible(result.visible)).catch(() => undefined)
    }, [session?.user.id, session?.user.role])
    async function logout() {
        try { await api<void>('/auth/logout', { method: 'POST' }, session?.csrfToken); setSession(null) }
        catch (e) { setError(e instanceof Error ? e.message : 'Could not sign out') }
    }
    if (checking || !setupStatus) return <main className="shell narrowShell" aria-busy="true"><p role="status">{error || 'Checking setup and your session…'}</p></main>
    if (setupStatus.required) return <SetupWizardPage session={session} onComplete={current => { setSession(current); setSetupStatus({ complete: true, required: false }); navigate('/get-started') }} />
    if (!session) return <main className="shell narrowShell"><section className="card"><h1>Sign in to Requestarr</h1><p>Sign in with your organization account to request and track availability.</p>{error && <p role="alert" className="error">{error}</p>}<a className="button" href="/api/v1/auth/login">Sign in</a></section></main>
    return <div className="appShell"><header className="rail"><a className="brand" href="/" onClick={followLink}><BrandMark />Requestarr</a><nav aria-label="Main navigation"><a className="navlink" href="/" onClick={followLink} aria-current={page === 'home' ? 'page' : undefined}>Request</a><a className="navlink" href="/library" onClick={followLink} aria-current={page === 'library' ? 'page' : undefined}>Library</a><a className="navlink" href="/requests" onClick={followLink} aria-current={page === 'requests' ? 'page' : undefined}>Requests</a><a className="navlink" href="/issues" onClick={followLink} aria-current={page === 'issues' ? 'page' : undefined}>Issues{session.user.role === 'admin' && openIssues > 0 && <span className="navCount" aria-label={`${openIssues} open`}>{openIssues}</span>}</a><a className="navlink" href="/notifications" onClick={followLink} aria-current={page === 'notifications' ? 'page' : undefined}>Notifications</a>{session.user.role === 'admin' && <>{(checklistVisible || page === 'get-started') && <a className="navlink" href="/get-started" onClick={followLink} aria-current={page === 'get-started' ? 'page' : undefined}>Get started</a>}<a className="navlink" href="/settings" onClick={followLink} aria-current={page === 'settings' ? 'page' : undefined}>Settings</a></>}</nav><div className="account"><span>{session.user.displayName || session.user.email}</span><button className="button secondary" onClick={() => void logout()}>Sign out</button></div></header><div className="appMain">{error && <p role="alert" className="shell error">{error}</p>}{page === 'get-started' ? session.user.role === 'admin' ? <GetStartedPage session={session} onChanged={setChecklistVisible} /> : <main className="shell"><section className="card"><h1>Administrator access required</h1><p>Only administrators can see the Get started checklist.</p></section></main> : page === 'home' ? <HomePage session={session} onCreated={() => navigate('/requests')} /> : page === 'library' ? <LibraryPage session={session} /> : page === 'issues' ? <IssuesPage session={session} onChanged={countIssues} /> : page === 'requests' ? <RequestsPage user={session.user} csrfToken={session.csrfToken} /> : page === 'settings' && session.user.role === 'admin' ? <SettingsPage session={session} /> : <NotificationsPage session={session} />}<footer className="attribution"><a href="https://www.themoviedb.org/" target="_blank" rel="noreferrer"><img src={tmdbLogo} alt="The Movie Database (TMDB)" /></a><p>This application uses TMDB and the TMDB APIs but is not endorsed, certified, or otherwise approved by TMDB.</p></footer></div></div>
}
