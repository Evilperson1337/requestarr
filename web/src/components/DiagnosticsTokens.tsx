import { useEffect, useId, useRef, useState, type FormEvent } from 'react'
import { api, type CreatedDiagnosticsToken, type DiagnosticsToken, type DiagnosticsTokenList } from '../api'
import { SettingsSection } from './SettingsSection'
import { useBusy } from './authCard'

const lifetimes = [[7, '7 days'], [30, '30 days'], [90, '90 days']] as const
const endpoint = '/admin/diagnostics-tokens'
const when = (iso: string) => new Date(iso).toLocaleString()

// Settings, Logs: diagnostics tokens (D043). A token lets a tool read the server's logs and
// status, nothing else. Its value comes back once, in the response that creates it; it is
// held in this component only until the superuser dismisses it, never in storage or a URL.
export function DiagnosticsTokens({ csrfToken, onRequestError }: { csrfToken: string; onRequestError: (error: unknown) => void }) {
    const id = useId()
    const [tokens, setTokens] = useState<DiagnosticsToken[] | null>(null)
    const [maxActive, setMaxActive] = useState(5)
    const [loadError, setLoadError] = useState('')
    const [name, setName] = useState('')
    const [days, setDays] = useState<number>(30)
    const [created, setCreated] = useState<CreatedDiagnosticsToken | null>(null)
    const [copied, setCopied] = useState('')
    const { busy, error, notice, setNotice, run } = useBusy<'create' | `revoke:${string}`>(onRequestError)
    const reveal = useRef<HTMLDivElement>(null)

    async function load() {
        try {
            const result = await api<DiagnosticsTokenList>(endpoint, { cache: 'no-store' })
            setTokens(result.tokens ?? [])
            if (result.maxActive) setMaxActive(result.maxActive)
            setLoadError('')
        } catch (e) {
            onRequestError(e)
            setLoadError(e instanceof Error ? e.message : 'Could not load the diagnostics tokens.')
        }
    }
    useEffect(() => { void load() }, []) // eslint-disable-line react-hooks/exhaustive-deps
    useEffect(() => { if (created) reveal.current?.focus() }, [created])

    function create(event: FormEvent<HTMLFormElement>) {
        event.preventDefault()
        void run('create', async () => {
            setCreated(null); setCopied('')
            const made = await api<CreatedDiagnosticsToken>(endpoint, { method: 'POST', body: JSON.stringify({ name: name.trim(), lifetimeDays: days }), cache: 'no-store' }, csrfToken)
            setCreated(made)
            setName('')
            await load()
        })
    }
    function revoke(token: DiagnosticsToken) {
        void run(`revoke:${token.id}`, async () => {
            await api<void>(`${endpoint}/${token.id}`, { method: 'DELETE', cache: 'no-store' }, csrfToken)
            if (created?.token.id === token.id) { setCreated(null); setCopied('') }
            setNotice(`Revoked ${token.name}. It stopped working at once.`)
            await load()
        })
    }
    async function copy() {
        try { await navigator.clipboard.writeText(created!.value); setCopied('Token copied.') } catch { setCopied('Could not copy. Select the token and copy it.') }
    }

    const now = Date.now()
    const active = (tokens ?? []).filter(token => new Date(token.expiresAt).getTime() > now).length
    return <SettingsSection title="Diagnostics tokens" description={<>A token lets a tool you run, such as <code>scripts/agent/requestarr-diag.sh</code>, read this server&apos;s logs, status, job runs and where each credential comes from, and nothing else: it cannot change anything, see people or requests, or read a secret. It stops working when it expires, when you revoke it, or when you stop being a superuser who can sign in. Up to {maxActive} may be active at once.</>}>
        <div aria-live="polite" className="authMessages">
            {error && <p role="alert" className="errorBox">{error}</p>}
            {notice && <p role="status" className="authOk">{notice}</p>}
        </div>
        {created && <div ref={reveal} role="group" tabIndex={-1} aria-labelledby={`${id}-once`} className="authNotice reveal">
            <h3 id={`${id}-once`}>New token: {created.token.name}</h3>
            <p><code className="oneTime">{created.value}</code></p>
            <p>This is the only time the token is shown. It is not stored and cannot be shown again; if you lose it, revoke it and make another. It expires {when(created.token.expiresAt)}.</p>
            <div className="draftActions">
                <button type="button" className="button secondary" onClick={() => void copy()}>Copy token</button>
                <button type="button" className="button" onClick={() => { setCreated(null); setCopied('') }}>I have saved the token</button>
            </div>
            <p role="status" className="authOk">{copied}</p>
        </div>}
        <form className="authForm" onSubmit={create}>
            <div className="authFields">
                <div className="field"><label htmlFor={`${id}-name`}>Token name</label><input id={`${id}-name`} type="text" maxLength={80} required autoComplete="off" value={name} onChange={e => setName(e.target.value)} placeholder="e.g. my laptop" /></div>
                <div className="field"><label htmlFor={`${id}-days`}>Lifetime</label><select id={`${id}-days`} value={days} onChange={e => setDays(Number(e.target.value))}>{lifetimes.map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select></div>
            </div>
            <div className="draftActions"><button className="button" type="submit" disabled={!!busy || name.trim() === '' || active >= maxActive}>{busy === 'create' ? 'Creating…' : 'Create token'}</button>{active >= maxActive && <span className="fieldHelp">Revoke a token to make another.</span>}</div>
        </form>
        {loadError && <p role="alert" className="errorBox">{loadError} <button type="button" className="button secondary" onClick={() => void load()}>Retry</button></p>}
        {tokens && tokens.length === 0 && <p>No diagnostics tokens.</p>}
        {tokens && tokens.length > 0 && <div className="table-wrap" role="region" aria-label="Diagnostics token list" tabIndex={0}><table>
            <thead><tr><th scope="col">Name</th><th scope="col">Created</th><th scope="col">Expires</th><th scope="col">Last used</th><th scope="col">Action</th></tr></thead>
            <tbody>{tokens.map(token => <tr key={token.id}>
                <td>{token.name}</td>
                <td><time dateTime={token.createdAt}>{when(token.createdAt)}</time></td>
                <td><time dateTime={token.expiresAt}>{when(token.expiresAt)}</time>{new Date(token.expiresAt).getTime() <= now && <> <span className="badge">Expired</span></>}</td>
                <td>{token.lastUsedAt ? <time dateTime={token.lastUsedAt}>{when(token.lastUsedAt)}</time> : 'Never'}</td>
                <td><button type="button" className="button secondary" disabled={!!busy} aria-label={`Revoke ${token.name}`} onClick={() => revoke(token)}>{busy === `revoke:${token.id}` ? 'Revoking…' : 'Revoke'}</button></td>
            </tr>)}</tbody>
        </table></div>}
    </SettingsSection>
}
