import { useCallback, useEffect, useId, useState, type FormEvent } from 'react'
import { api, type ConnectionTest, type LibraryConnection, type OperationalStatus } from '../api'
import { IntegrationCard, type Tone } from './IntegrationCard'
import { SecretField } from './SecretField'

type Source = OperationalStatus['sources'][number]

function summary(c: LibraryConnection): { text: string; tone: Tone } {
    if (c.active) return { text: c.urlFromEnvironment ? `Syncing · address set by ${c.urlEnv}` : 'Syncing', tone: 'on' }
    if (c.url && c.missing.length) return { text: `Off · needs ${c.missing.join(' and ')}`, tone: 'warn' }
    if (c.urlFromEnvironment) return { text: `Off · address set by ${c.urlEnv}`, tone: 'warn' }
    return { text: 'Not set up', tone: 'off' }
}

const when = (value: string | null | undefined) => (value ? new Date(value).toLocaleString() : 'Never')

function Connection({ connection, source, syncing, onSync, csrfToken, reload, onRequestError }: { connection: LibraryConnection; source?: Source; syncing: boolean; onSync: (slug: string) => void; csrfToken: string; reload: () => Promise<void>; onRequestError: (error: unknown) => void }) {
    const id = useId()
    const [url, setURL] = useState(connection.url)
    const [username, setUsername] = useState(connection.username)
    const [secrets, setSecrets] = useState<Record<string, string>>({})
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
    const typed = Object.entries(secrets).filter(([, value]) => value !== '')
    const addressDirty = url.trim() !== connection.url || (connection.takesUsername && username.trim() !== connection.username)
    function save(event: FormEvent) {
        event.preventDefault()
        if (busy || ((!addressDirty || locked) && !typed.length)) return
        // Only what changed is sent: an empty credential field keeps the saved one,
        // and an address set by the environment is never sent.
        const body: { url?: string; username?: string; credentials?: Record<string, string> } = {}
        if (!locked && addressDirty) { body.url = url.trim(); if (connection.takesUsername) body.username = username.trim() }
        if (typed.length) body.credentials = Object.fromEntries(typed)
        setSecrets({}) // never keep a secret in the page after submit, whether or not the save works
        void run('save', async () => {
            await api<LibraryConnection>(base, { method: 'PUT', body: JSON.stringify(body) }, csrfToken)
            await reload(); setNotice(`${connection.name} saved and applied.`)
        }, `${connection.name} could not be saved.`)
    }
    function remove() {
        void run('remove', async () => { await api<LibraryConnection>(base, { method: 'DELETE' }, csrfToken); await reload(); setNotice(`${connection.name} removed.`) }, `${connection.name} could not be removed.`)
    }
    function probe() {
        void run('test', async () => { setTest(await api<ConnectionTest>(`${base}/test`, { method: 'POST' }, csrfToken)) }, `${connection.name} could not be tested.`)
    }
    const dirty = (addressDirty && !locked) || typed.length > 0
    const { text, tone } = summary(connection)
    const meta = connection.active && source
        ? <>Last sync {when(source.lastRun?.startedAt)} · {source.lastRun?.status ?? 'not run yet'}{source.lastSuccessAt ? <> · last success {when(source.lastSuccessAt)}</> : null}{source.lastError ? <> · <span className="error">{source.lastError}</span></> : null}</>
        : undefined
    const sync = connection.active ? <button className="button secondary" type="button" aria-label={`Sync ${connection.name} now`} disabled={syncing} onClick={() => onSync(connection.slug)}>{syncing ? 'Syncing…' : 'Sync now'}</button> : undefined
    return <IntegrationCard slug={connection.slug} name={connection.name} status={text} tone={tone} meta={meta} actions={sync} setUp={!connection.url && !connection.active}>
        <form onSubmit={save}>
            <label htmlFor={`${id}-url`}>Address</label>
            <input id={`${id}-url`} type="url" inputMode="url" autoComplete="off" spellCheck={false} maxLength={2048} placeholder="https://" value={url} disabled={locked || !!busy} aria-describedby={`${id}-help`} onChange={event => setURL(event.target.value)} />
            <p id={`${id}-help`}>{locked ? <>Set by <code>{connection.urlEnv}</code> on the server. Change it there.</> : 'The address Requestarr reaches this server at, such as http://192.168.1.20:8096.'}</p>
            {connection.takesUsername && <>
                <label htmlFor={`${id}-user`}>Username</label>
                <input id={`${id}-user`} autoComplete="off" spellCheck={false} maxLength={256} value={username} disabled={locked || !!busy} onChange={event => setUsername(event.target.value)} />
            </>}
            {connection.credentials.map(credential => <SecretField key={credential.secret} name={credential.secret} label={credential.label || credential.secret} csrfToken={csrfToken} onRequestError={onRequestError} appliesImmediately onChanged={() => void reload()} entry={{ value: secrets[credential.secret] ?? '', onChange: value => setSecrets(current => ({ ...current, [credential.secret]: value })), disabled: !!busy }} />)}
            <div className="draftActions">
                <button className="button" type="submit" disabled={!!busy || !dirty || (!locked && !url.trim())}>{busy === 'save' ? 'Saving…' : 'Save and apply'}</button>
                <button className="button secondary" type="button" disabled={!!busy || !connection.url} onClick={probe}>{busy === 'test' ? 'Testing…' : 'Test connection'}</button>
                {connection.saved && !locked && <button className="button secondary" type="button" disabled={!!busy} onClick={remove}>Remove</button>}
            </div>
        </form>
        <div aria-live="polite">
            {notice && <p role="status">{notice}</p>}
            {test && <p role="status" className={test.ok ? 'testOk' : 'error'}>{test.message}</p>}
        </div>
        {error && <p role="alert" className="error">{error}</p>}
    </IntegrationCard>
}

// Where each library Source lives (D028). Changes apply at once; an address set
// by an environment variable is shown but cannot be changed here.
export function LibraryConnections({ csrfToken, onRequestError, onLoaded, sources, syncing, onSync }: { csrfToken: string; onRequestError: (error: unknown) => void; onLoaded?: (secretNames: string[]) => void; sources: Source[]; syncing: string; onSync: (slug: string) => void }) {
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
    return <ul className="integrations" aria-label="Library connections">{connections.map(c => <Connection key={c.slug} connection={c} source={sources.find(item => item.slug === c.slug)} syncing={syncing !== ''} onSync={onSync} csrfToken={csrfToken} reload={reload} onRequestError={onRequestError} />)}</ul>
}
