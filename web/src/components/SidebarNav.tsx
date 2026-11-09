import { useState, type ReactNode } from 'react'
import { followLink } from '../navigation'
import { BrandMark } from './BrandMark'
import type { Session } from '../api'

export type Page = 'home' | 'library' | 'requests' | 'issues' | 'notifications' | 'get-started' | 'settings' | 'logs'

// Navigation icons: decorative, the label always sits beside them.
const icons: Record<Page, ReactNode> = {
    home: <path d="M3 11.5 12 4l9 7.5M5.5 10v10h13V10" />,
    library: <path d="M4 5h4v14H4zM10 5h4v14h-4zM16.5 5.5l3.7 1-3.4 13-3.7-1z" />,
    requests: <path d="M5 4h14v16H5zM8.5 9h7M8.5 13h7M8.5 17h4" />,
    issues: <path d="M12 3.5 21 19.5H3zM12 10v4.5M12 17v.2" />,
    notifications: <path d="M6 17V11a6 6 0 0 1 12 0v6l1.5 2h-15zM10 21h4" />,
    'get-started': <path d="M5 12.5 10 17.5 19 7" />,
    settings: <path d="M12 8.8a3.2 3.2 0 1 0 0 6.4 3.2 3.2 0 0 0 0-6.4zM12 3v2.5M12 18.5V21M3 12h2.5M18.5 12H21M5.6 5.6l1.8 1.8M16.6 16.6l1.8 1.8M5.6 18.4l1.8-1.8M16.6 7.4l1.8-1.8" />,
    logs: <path d="M4 6.5l5 4.5-5 4.5M12 17.5h8" />,
}

function NavIcon({ page }: { page: Page }) {
    return <svg className="navIcon" viewBox="0 0 24 24" aria-hidden="true" focusable="false" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round">{icons[page]}</svg>
}

type Counts = { awaitingApproval: number; openIssues: number; unread: number }

// The sidebar is a permanent rail on wide screens and a top bar with a drawer below 900px.
export function SidebarNav({ session, page, counts, checklistVisible, onSignOut }: { session: Session; page: Page; counts: Counts; checklistVisible: boolean; onSignOut: () => void }) {
    const [open, setOpen] = useState(false)
    const caps = session.capabilities
    const link = (target: Page, href: string, label: string, count = 0, countLabel = '') => <a key={target} className="navlink" href={href} onClick={event => { followLink(event); setOpen(false) }} aria-current={page === target ? 'page' : undefined}><NavIcon page={target} />{label}{count > 0 && <span className="navCount" aria-label={`${count} ${countLabel}`}>{count}</span>}</a>
    return <header className="rail" data-open={open}>
        <div className="railTop">
            <a className="brand" href="/" onClick={followLink}><BrandMark />Requestarr</a>
            <button type="button" className="navToggle" aria-expanded={open} aria-controls="rail-panel" onClick={() => setOpen(value => !value)}>{open ? 'Close menu' : 'Menu'}</button>
        </div>
        <div className="railPanel" id="rail-panel">
            <nav aria-label="Main navigation">
                {link('home', '/', 'Request')}
                {link('library', '/library', 'Library')}
                {link('requests', caps.manageRequests && counts.awaitingApproval > 0 ? '/requests?status=requested' : '/requests', 'Requests', caps.manageRequests ? counts.awaitingApproval : 0, 'waiting for approval')}
                {link('issues', '/issues', 'Issues', caps.manageRequests ? counts.openIssues : 0, 'open')}
                {link('notifications', '/notifications', 'Notifications', counts.unread, 'unread')}
                {caps.manageSystem && <>
                    {caps.manageAccess && (checklistVisible || page === 'get-started') && link('get-started', '/get-started', 'Get started')}
                    {link('settings', '/settings', 'Settings')}
                    {link('logs', '/logs', 'Logs')}
                </>}
            </nav>
            <div className="account"><span>{session.user.displayName || session.user.email}</span><button className="button secondary" onClick={onSignOut}>Sign out</button></div>
        </div>
    </header>
}
