import { useCallback, useEffect, useState } from 'react'
import { api, ApiError, type Issue, type IssueDetail, type IssueStatus, type Selection, type Session } from '../api'
import { followBackLink, followLink, navigate, useLocation } from '../navigation'
import { describeSelection } from '../selection'

const statuses = [['open', 'Open'], ['in_progress', 'In progress'], ['resolved', 'Resolved'], ['closed', 'Closed'], ['', 'All']] as const
const statusLabels: Record<IssueStatus, string> = { open: 'Open', in_progress: 'In progress', resolved: 'Resolved', closed: "Closed (won't fix)" }
const cover = (type: string) => type === 'album' || type === 'song' ? ' square' : type === 'game' ? ' game' : ''
const when = (value: string) => new Date(value).toLocaleString()
const part = (p: Selection) => describeSelection(p)

function StatusBadge({ status }: { status: IssueStatus }) {
    return <span className={`badge status-${status === 'open' ? 'requested' : status === 'in_progress' ? 'approved' : status === 'resolved' ? 'available' : 'cancelled'}`}>{statusLabels[status].toLowerCase()}</span>
}

// Reports of problems with items a library holds (D030). Administrators see every one; everyone else, their own.
export function IssuesPage({ session, onChanged }: { session: Session; onChanged: () => void }) {
    const view = useLocation()
    const params = new URLSearchParams(view.includes('?') ? view.slice(view.indexOf('?')) : '')
    const issueId = params.get('issueId')
    return issueId ? <IssueView id={issueId} session={session} onChanged={onChanged} /> : <IssueList session={session} />
}

function IssueList({ session }: { session: Session }) {
    const [status, setStatus] = useState<string>('open')
    const [items, setItems] = useState<Issue[]>([])
    const [next, setNext] = useState<string | null>(null)
    const [loading, setLoading] = useState(true)
    const [error, setError] = useState('')
    const admin = session.capabilities.manageRequests
    const load = useCallback(async (cursor: string | null) => {
        setLoading(true); setError('')
        const q = new URLSearchParams({ limit: '25' })
        if (status) q.set('status', status)
        if (cursor) q.set('cursor', cursor)
        try {
            const page = await api<{ items: Issue[]; nextCursor: string | null }>(`/issues?${q}`, { cache: 'no-store' })
            setItems(current => cursor ? [...current, ...page.items] : page.items); setNext(page.nextCursor)
        } catch (e) { setError(e instanceof Error ? e.message : 'Issues could not be loaded.') }
        finally { setLoading(false) }
    }, [status])
    useEffect(() => { setItems([]); void load(null) }, [load])
    return <main className="shell issuesPage">
        <h1>Issues</h1>
        <p className="libFacts">{admin ? 'Problems people have reported with items in your libraries.' : 'Problems you have reported with items in the libraries.'}</p>
        <div className="settingsTabs" role="group" aria-label="Issue status">{statuses.map(([key, label]) => <button key={key} type="button" className="settingsTab" aria-pressed={status === key} onClick={() => setStatus(key)}>{label}</button>)}</div>
        {error && <p role="alert" className="error">{error}</p>}
        {!loading && !error && items.length === 0 && <p>{status === 'open' ? 'No open issues.' : 'No issues here.'}{!admin && ' Report one from an item in the Library.'}</p>}
        <ul className="issueList" aria-label="Issues" aria-busy={loading}>{items.map(issue => <li key={issue.id}>
            <a className="issueRow" href={`/issues?issueId=${issue.id}`} onClick={followLink}>
                <span className="issueThumb" aria-hidden="true">{issue.artworkUrl ? <img src={issue.artworkUrl} alt="" loading="lazy" /> : null}</span>
                <span className="issueMain"><strong>{issue.title}</strong><span>{issue.kindLabel}{part(issue.part) ? ` · ${part(issue.part)}` : ''}</span></span>
                <span className="issueMeta">{issue.goneFromLibrary && <span className="tag">No longer in the library</span>}<span>{issue.reporters} {issue.reporters === 1 ? 'reporter' : 'reporters'}</span><time dateTime={issue.createdAt}>{new Date(issue.createdAt).toLocaleDateString()}</time><StatusBadge status={issue.status} /></span>
            </a>
        </li>)}</ul>
        {loading && <p role="status">Loading…</p>}
        {next && !loading && <div className="draftActions"><button className="button secondary" type="button" onClick={() => void load(next)}>Show more</button></div>}
    </main>
}

function IssueView({ id, session, onChanged }: { id: string; session: Session; onChanged: () => void }) {
    const [detail, setDetail] = useState<IssueDetail | null>(null)
    const [error, setError] = useState('')
    const [missing, setMissing] = useState(false)
    const [comment, setComment] = useState('')
    const [closing, setClosing] = useState('')
    const [target, setTarget] = useState<IssueStatus>('open')
    const [busy, setBusy] = useState(false)
    const admin = session.capabilities.manageRequests
    const load = useCallback(async () => {
        try { setDetail(await api<IssueDetail>(`/issues/${encodeURIComponent(id)}`, { cache: 'no-store' })); setMissing(false) }
        catch (e) { if (e instanceof ApiError && e.status === 404) setMissing(true); else setError(e instanceof Error ? e.message : 'The issue could not be loaded.') }
    }, [id])
    useEffect(() => { setDetail(null); void load() }, [load])
    useEffect(() => { if (detail) setTarget(detail.issue.status) }, [detail?.issue.status, detail?.issue.id])
    async function act(work: () => Promise<unknown>, fail: string) {
        if (busy) return
        setBusy(true); setError('')
        try { await work(); await load(); onChanged() } catch (e) { setError(e instanceof Error && e.message ? e.message : fail) } finally { setBusy(false) }
    }
    const back = <a className="button secondary" href="/issues" onClick={followBackLink}>All issues</a>
    if (missing) return <main className="shell"><section className="card"><h1>Issue not found</h1><p>It may have been removed, or it is not one you reported.</p>{back}</section></main>
    if (!detail) return <main className="shell" aria-busy="true"><p role={error ? 'alert' : 'status'} className={error ? 'error' : undefined}>{error || 'Loading…'}</p></main>
    const { issue } = detail
    const status = (to: IssueStatus) => act(() => api(`/issues/${issue.id}/status`, { method: 'POST', body: JSON.stringify({ status: to, comment: closing.trim() || undefined }) }, session.csrfToken).then(() => setClosing('')), 'The status could not be changed.')
    return <main className="shell issuePage">
        <div className="issueTop">
            <span className={`railCover issueCover${cover(issue.mediaType)}`}>{issue.artworkUrl ? <img src={issue.artworkUrl} alt={`Cover for ${issue.title}`} /> : <span className="railBlank" aria-hidden="true">{issue.mediaType === 'tv' ? 'TV' : issue.mediaType}</span>}</span>
            <div>
                <div className="issueHead"><h1>{issue.title}{issue.year ? ` (${issue.year})` : ''}</h1><StatusBadge status={issue.status} /></div>
                <p className="libFacts">{issue.kindLabel}{part(issue.part) ? ` · ${part(issue.part)}` : ''}</p>
                <p className="libFacts">{issue.source} · opened {when(issue.createdAt)} · {issue.reporters} {issue.reporters === 1 ? 'reporter' : 'reporters'}</p>
                {issue.goneFromLibrary && <p role="status" className="tag">No longer in the library</p>}
                {issue.link && <p><a className="button secondary" href={issue.link} target="_blank" rel="noreferrer">Open in {issue.source}</a></p>}
            </div>
        </div>
        {admin && <section aria-labelledby="decide-heading"><h2 id="decide-heading">Status</h2>
            <div className="issueStatusForm">
                <label>Status<select value={target} disabled={busy} onChange={event => setTarget(event.target.value as IssueStatus)}>{(Object.keys(statusLabels) as IssueStatus[]).map(key => <option key={key} value={key}>{statusLabels[key]}</option>)}</select></label>
                <label className="field">Note (optional)<textarea maxLength={2000} value={closing} disabled={busy} onChange={event => setClosing(event.target.value)} /></label>
            </div>
            <div className="draftActions"><button className="button" type="button" disabled={busy || target === issue.status} onClick={() => void status(target)}>Update status</button></div></section>}
        <section aria-labelledby="timeline-heading"><h2 id="timeline-heading">Timeline</h2>
            <ol className="timeline">{detail.events.map(event => <li key={event.id}><strong>{event.actorName}</strong> {({ reported: 'reported this', reporter_added: 'reported it too', commented: 'commented', in_progress: 'started working on it', resolved: 'resolved it', closed: "closed it (won't fix)", reopened: 'set it back to open', left: 'stopped following' } as Record<string, string>)[event.type] ?? event.type}<time dateTime={event.at}> · {when(event.at)}</time>{event.comment && <p>{event.comment}</p>}</li>)}</ol>
        </section>
        {admin && detail.reporters.length > 0 && <section aria-labelledby="reporters-heading"><h2 id="reporters-heading">Reporters</h2>
            <ul>{detail.reporters.map(reporter => <li key={reporter.userId}>{reporter.name}{reporter.left ? ' (stopped following)' : ''}{reporter.note ? `: ${reporter.note}` : ''}</li>)}</ul></section>}
        {(admin || detail.youReported) && <form className="issueComment" onSubmit={event => { event.preventDefault(); if (comment.trim()) void act(() => api(`/issues/${issue.id}/comments`, { method: 'POST', body: JSON.stringify({ text: comment.trim() }) }, session.csrfToken).then(() => setComment('')), 'The comment could not be sent.') }}>
            <label className="field">Add a comment<textarea maxLength={2000} value={comment} disabled={busy} onChange={event => setComment(event.target.value)} /></label>
            <div className="draftActions"><button className="button secondary" type="submit" disabled={busy || !comment.trim()}>Comment</button></div>
        </form>}
        {detail.youReported && (issue.status === 'open' || issue.status === 'in_progress') && !admin && <div className="draftActions"><button className="button secondary" type="button" disabled={busy} onClick={() => void act(() => api(`/issues/${issue.id}/reporter`, { method: 'DELETE' }, session.csrfToken).then(() => navigate('/issues')), 'Could not stop following.')}>Stop following</button></div>}
        {error && <p role="alert" className="error">{error}</p>}
        <div className="draftActions">{back}</div>
    </main>
}
