import { useCallback, useEffect, useId, useState, type FormEvent } from 'react'
import { api, type ConnectionTest, type LibraryConnection } from '../api'
import { SecretField } from './SecretField'

function status(c: LibraryConnection): string {
    if (c.urlFromEnvironment && c.active) return `Syncing · address set by ${c.urlEnv}`
    if (c.active) return 'Syncing'
    if (c.url && c.missing.length) return `Off · needs ${c.missing.join(' and ')}`
    if (c.urlFromEnvironment) return `Off · address set by ${c.urlEnv}`
    return 'Not set up'
}

function Connection({ connection, csrfToken, reload, onRequestError }: { connection: LibraryConnection; csrfToken: string; reload: () => Promise<void>; onRequestError: (error: unknown) => void }) {
    const id = useId()
    const [url, setURL] = useState(connection.url)
    const [username, setUsername] = useState(connection.username)
    const [busy, setBusy] = useState('')
    const [error, setError] = useState('')
    const [notice, setNotice] = useState('')
    const [test, setTest] = useState<ConnectionTest | null>(null)
    useEffect(() => { setURL(connection.url); setUsername(connection.username) }, [connection.url, connection.username])
    const locked = connection.urlFromEnvironment
    const base = `/admin/library-connections/${connection.slug}`
    async function run(kind: string, work: () => Promise<void>, fail: string) {
        if (busy) return
        setBusy(kind); setError(''); setNotice(''); setTest(null)
        try { await work() } catch (e) { onRequestError(e); setError(e instanceof Error && e.message ? e.message : fail) } finally { setBusy('') }
    }
    function save(event: FormEvent) {
        event.preventDefault()
        void run('save', async () => {
            await api<LibraryConnection>(base, { method: 'PUT', body: JSON.stringify(connection.takesUsername ? { url: url.trim(), username: username.trim() } : { url: url.trim() }) }, csrfToken)
            await reload(); setNotice(`${connection.name} saved and applied.`)
        }, `${connection.name} could not be saved.`)
    }
    function remove() {
        void run('remove', async () => { await api<LibraryConnection>(base, { method: 'DELETE' }, csrfToken); await reload(); setNotice(`${connection.name} removed.`) }, `${connection.name} could not be removed.`)
    }
    function probe() {
        void run('test', async () => { setTest(await api<ConnectionTest>(`${base}/test`, { method: 'POST' }, csrfToken)) }, `${connection.name} could not be tested.`)
    }
    const dirty = url.trim() !== connection.url || (connection.takesUsername && username.trim() !== connection.username)
    return <li className="connection" aria-labelledby={`${id}-name`}>
        <div className="connectionHead"><h3 id={`${id}-name`}>{connection.name}</h3><span className={`connectionState${connection.active ? ' on' : ''}`}>{status(connection)}</span></div>
        <form onSubmit={save}>
            <label htmlFor={`${id}-url`}>Address</label>
            <input id={`${id}-url`} type="url" inputMode="url" autoComplete="off" spellCheck={false} maxLength={2048} placeholder="https://" value={url} disabled={locked || !!busy} aria-describedby={`${id}-help`} onChange={event => setURL(event.target.value)} />
            <p id={`${id}-help`}>{locked ? <>Set by <code>{connection.urlEnv}</code> on the server. Change it there.</> : 'The address Requestarr reaches this server at, such as http://192.168.1.20:8096.'}</p>
            {connection.takesUsername && <>
                <label htmlFor={`${id}-user`}>Username</label>
                <input id={`${id}-user`} autoComplete="off" spellCheck={false} maxLength={256} value={username} disabled={locked || !!busy} onChange={event => setUsername(event.target.value)} />
            </>}
            <div className="draftActions">
                <button className="button" type="submit" disabled={locked || !!busy || !url.trim() || !dirty}>{busy === 'save' ? 'Saving…' : 'Save and apply'}</button>
                <button className="button secondary" type="button" disabled={!!busy || !connection.url} onClick={probe}>{busy === 'test' ? 'Testing…' : 'Test connection'}</button>
                {connection.saved && !locked && <button className="button secondary" type="button" disabled={!!busy} onClick={remove}>Remove</button>}
            </div>
        </form>
        {connection.credentials.map(credential => <SecretField key={credential.secret} name={credential.secret} label={credential.label || credential.secret} csrfToken={csrfToken} onRequestError={onRequestError} appliesImmediately onChanged={() => void reload()} />)}
        <div aria-live="polite">
            {notice && <p role="status">{notice}</p>}
            {test && <p role="status" className={test.ok ? 'testOk' : 'error'}>{test.ok ? 'Connected. ' : ''}{test.message}</p>}
        </div>
        {error && <p role="alert" className="error">{error}</p>}
    </li>
}

// Where each library Source lives (D028). Changes apply at once; an address set
// by an environment variable is shown but cannot be changed here.
export function LibraryConnections({ csrfToken, onRequestError, onLoaded }: { csrfToken: string; onRequestError: (error: unknown) => void; onLoaded?: (secretNames: string[]) => void }) {
    const [connections, setConnections] = useState<LibraryConnection[] | null>(null)
    const [failed, setFailed] = useState(false)
    const reload = useCallback(async () => {
        try {
            const result = await api<{ connections: LibraryConnection[] }>('/admin/library-connections', { cache: 'no-store' })
            if (!Array.isArray(result?.connections)) throw new Error('unexpected response')
            setConnections(result.connections); setFailed(false)
            onLoaded?.(result.connections.flatMap(c => c.credentials.map(credential => credential.secret)))
        } catch (e) { onRequestError(e); setFailed(true) }
    }, [onRequestError, onLoaded])
    useEffect(() => { void reload() }, [reload])
    if (failed) return <p role="alert" className="error">Library connections could not be loaded.</p>
    if (!connections) return <p role="status">Loading library connections…</p>
    return <ul className="connections" aria-label="Library connections">{connections.map(c => <Connection key={c.slug} connection={c} csrfToken={csrfToken} reload={reload} onRequestError={onRequestError} />)}</ul>
}
