import { useEffect, useState } from 'react'
import { api, type Session } from './api'
import { HomePage } from './pages/HomePage'
import { RequestsPage } from './pages/RequestsPage'
import { NotificationsPage } from './pages/NotificationsPage'
import { LibraryPage } from './pages/LibraryPage'
import { IssuesPage } from './pages/IssuesPage'
import { SettingsPage } from './pages/SettingsPage'
import { GetStartedPage } from './pages/GetStartedPage'
import { SignInPage } from './pages/SignInPage'
import { ChangePasswordPage } from './pages/ChangePasswordPage'
import tmdbLogo from './assets/tmdb-attribution.svg'
import { followLink, navigate, useLocation } from './navigation'
import { BrandMark } from './components/BrandMark'

export function App() {
    const [session, setSession] = useState<Session | null>(null)
    const [checking, setChecking] = useState(true)
    const path = useLocation().split('?')[0].replace(/\/$/, '')
    const page = path === '/get-started' ? 'get-started' : path === '/library' ? 'library' : path === '/issues' ? 'issues' : path === '/requests' ? 'requests' : path === '/notifications' ? 'notifications' : path === '/settings' ? 'settings' : 'home'
    const [error, setError] = useState('')
    const [openIssues, setOpenIssues] = useState(0)
    // The Get started checklist is offered to administrators until it is done or dismissed (D038).
    const [checklistVisible, setChecklistVisible] = useState(false)
    const [awaitingApproval, setAwaitingApproval] = useState(0)
    const refresh = async () => {
        setChecking(true)
        try {
            const current = await api<Session>('/auth/me', { cache: 'no-store' }).catch(() => null)
            setSession(current)
        } catch {
            setError('Could not check your session. Refresh the page to try again.')
        } finally { setChecking(false) }
    }
    useEffect(() => { void refresh() }, [])
    // Only administrators have a queue to work through, so only they get a count (D030).
    const countIssues = () => { if (session?.capabilities.manageRequests) api<{ open: number }>('/issues/summary', { cache: 'no-store' }).then(result => setOpenIssues(result.open)).catch(() => undefined) }
    useEffect(() => { countIssues() }, [session?.user.id, page])
    useEffect(() => {
        setChecklistVisible(false)
        if (session?.capabilities.manageSystem) api<{ visible: boolean }>('/admin/checklist', { cache: 'no-store' }).then(result => setChecklistVisible(result.visible)).catch(() => undefined)
    }, [session?.user.id, session?.user.role])
    // The same for requests still waiting for approval (D036); a creation or a decision refreshes it.
    const countAwaiting = () => { if (session?.capabilities.manageRequests) api<{ requested: number }>('/requests/summary', { cache: 'no-store' }).then(result => setAwaitingApproval(Number(result.requested) || 0)).catch(() => undefined) }
    useEffect(() => { countAwaiting() }, [session?.user.id, page])
    const [unread, setUnread] = useState(0)
    const countUnread = () => { if (session) api<{ unread: number }>('/notifications/summary', { cache: 'no-store' }).then(result => setUnread(Number(result.unread) || 0)).catch(() => undefined) }
    useEffect(() => { countUnread() }, [session?.user.id, page])
    async function logout() {
        try { await api<void>('/auth/logout', { method: 'POST' }, session?.csrfToken); setSession(null) }
        catch (e) { setError(e instanceof Error ? e.message : 'Could not sign out') }
    }
    if (checking) return <main className="shell narrowShell" aria-busy="true"><p role="status">{error || 'Checking your session…'}</p></main>
    // The local administrator must choose their own password first; the server refuses everything else.
    if (session?.mustChangePassword) return <ChangePasswordPage session={session} onChanged={() => void refresh()} onSignOut={() => void logout()} />
    // The local sign-in has its own address and is always served, whatever the identity-provider state.
    if (!session) return <SignInPage path={path} onSignedIn={() => void refresh()} error={error} />
    return <div className="appShell"><header className="rail"><a className="brand" href="/" onClick={followLink}><BrandMark />Requestarr</a><nav aria-label="Main navigation"><a className="navlink" href="/" onClick={followLink} aria-current={page === 'home' ? 'page' : undefined}>Request</a><a className="navlink" href="/library" onClick={followLink} aria-current={page === 'library' ? 'page' : undefined}>Library</a><a className="navlink" href={session.capabilities.manageRequests && awaitingApproval > 0 ? '/requests?status=requested' : '/requests'} onClick={followLink} aria-current={page === 'requests' ? 'page' : undefined}>Requests{session.capabilities.manageRequests && awaitingApproval > 0 && <span className="navCount" aria-label={`${awaitingApproval} waiting for approval`}>{awaitingApproval}</span>}</a><a className="navlink" href="/issues" onClick={followLink} aria-current={page === 'issues' ? 'page' : undefined}>Issues{session.capabilities.manageRequests && openIssues > 0 && <span className="navCount" aria-label={`${openIssues} open`}>{openIssues}</span>}</a><a className="navlink" href="/notifications" onClick={followLink} aria-current={page === 'notifications' ? 'page' : undefined}>Notifications{unread > 0 && <span className="navCount" aria-label={`${unread} unread`}>{unread}</span>}</a>{session.capabilities.manageSystem && <>{(checklistVisible || page === 'get-started') && <a className="navlink" href="/get-started" onClick={followLink} aria-current={page === 'get-started' ? 'page' : undefined}>Get started</a>}<a className="navlink" href="/settings" onClick={followLink} aria-current={page === 'settings' ? 'page' : undefined}>Settings</a></>}</nav><div className="account"><span>{session.user.displayName || session.user.email}</span><button className="button secondary" onClick={() => void logout()}>Sign out</button></div></header><div className="appMain">{error && <p role="alert" className="shell error">{error}</p>}{page === 'get-started' ? session.capabilities.manageSystem ? <GetStartedPage session={session} onChanged={setChecklistVisible} /> : <main className="shell"><section className="card"><h1>Administrator access required</h1><p>Only administrators can see the Get started checklist.</p></section></main> : page === 'home' ? <HomePage session={session} onCreated={() => navigate('/requests')} /> : page === 'library' ? <LibraryPage session={session} /> : page === 'issues' ? <IssuesPage session={session} onChanged={countIssues} /> : page === 'requests' ? <RequestsPage user={session.user} canManage={session.capabilities.manageRequests} csrfToken={session.csrfToken} onChanged={countAwaiting} /> : page === 'settings' && session.capabilities.manageSystem ? <SettingsPage session={session} /> : <NotificationsPage session={session} onChanged={countUnread} />}<footer className="attribution"><a href="https://www.themoviedb.org/" target="_blank" rel="noreferrer"><img src={tmdbLogo} alt="The Movie Database (TMDB)" /></a><p>This application uses TMDB and the TMDB APIs but is not endorsed, certified, or otherwise approved by TMDB.</p></footer></div></div>
}
