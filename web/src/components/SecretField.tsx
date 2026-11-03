import { useCallback, useEffect, useId, useState } from 'react'
import { api, type SecretStatus, type SecretsOverview } from '../api'

// Write-only entry for one stored secret (D024). The value lives only in this
// input until submit, is cleared whether or not the save succeeds, and is never
// put in browser storage, the URL, or the setup draft.
export function SecretField({ name, label, csrfToken, onRequestError, appliesImmediately, onChanged }: { name: string; label: string; csrfToken: string; onRequestError?: (error: unknown) => void; appliesImmediately?: boolean; onChanged?: () => void }) {
    const id = useId()
    const [overview, setOverview] = useState<SecretsOverview | null>(null)
    const [value, setValue] = useState('')
    const [busy, setBusy] = useState(false)
    const [error, setError] = useState('')
    const [notice, setNotice] = useState('')

    const refresh = useCallback(async () => {
        try {
            const result = await api<SecretsOverview>('/admin/secrets', { cache: 'no-store' })
            if (!result || typeof result.enabled !== 'boolean' || !Array.isArray(result.secrets)) throw new Error('unexpected response')
            setOverview(result)
        } catch (e) {
            onRequestError?.(e)
            setOverview(null); setError('Stored secret status could not be loaded.')
        }
    }, [onRequestError])
    useEffect(() => { void refresh() }, [refresh])

    const status: SecretStatus | undefined = overview?.secrets.find(item => item.name === name)

    async function save() {
        if (!value || busy) return
        setBusy(true); setError(''); setNotice('')
        const submitted = value
        setValue('') // never keep the secret in the page after submit
        try {
            await api<void>(`/admin/secrets/${name}`, { method: 'PUT', body: JSON.stringify({ value: submitted }) }, csrfToken)
            setNotice(appliesImmediately ? `${label} saved and applied.` : `${label} saved. It applies after Requestarr restarts.`)
            await refresh(); onChanged?.()
        } catch (e) {
            onRequestError?.(e)
            setError(`${label} could not be saved.`)
        } finally { setBusy(false) }
    }
    async function clear() {
        if (busy) return
        setBusy(true); setError(''); setNotice('')
        try {
            await api<void>(`/admin/secrets/${name}`, { method: 'DELETE' }, csrfToken)
            setNotice(appliesImmediately ? `Stored ${label} cleared and applied.` : `Stored ${label} cleared. This applies after Requestarr restarts.`)
            await refresh(); onChanged?.()
        } catch (e) {
            onRequestError?.(e)
            setError(`${label} could not be cleared.`)
        } finally { setBusy(false) }
    }

    const disabled = !overview || !overview.enabled
    return <div className="secretField">
        <label htmlFor={id}>{label}</label>
        <input id={id} type="password" autoComplete="off" autoCorrect="off" autoCapitalize="off" spellCheck={false} maxLength={4096}
            value={value} disabled={disabled || busy} aria-describedby={`${id}-help`}
            placeholder={status?.configured ? 'Saved. Enter a new value to replace it' : ''}
            onChange={event => setValue(event.target.value)}
            onKeyDown={event => { if (event.key === 'Enter') { event.preventDefault(); void save() } }} />
        <div className="draftActions">
            <button className="button secondary" type="button" disabled={disabled || busy || !value} onClick={() => void save()}>Save {label}</button>
            {status && (status.source !== 'none' && status.source !== 'environment') && <button className="button secondary" type="button" disabled={busy} onClick={() => void clear()}>Clear stored value</button>}
        </div>
        <p id={`${id}-help`}>
            {name === 'tmdb_read_token' && <>Use the API Read Access Token from TMDB API settings, not the v3 API key. Paste only the token, without a Bearer prefix. </>}
            {overview && !overview.enabled
                ? <>Saving secrets here is turned off. Set <code>REQUESTARR_SECRETS_KEY</code> (32 bytes as 64 hex characters or base64) on the server and restart, or set <code>{status?.envVar ?? 'the environment variable'}</code> there instead.</>
                : <>The value is stored encrypted and never shown again, and {appliesImmediately ? 'applies at once' : 'applies after a restart'}. An environment value always wins.</>}
        </p>
        {status && <p>Status: {status.unreadable ? 'stored secret unreadable. Enter it again or restore REQUESTARR_SECRETS_KEY' : status.configured ? 'configured' : 'not configured'} · Source: {status.source}{status.pendingRestart ? ' · Saved, pending restart' : ''}</p>}
        <div aria-live="polite">{notice && <p role="status">{notice}</p>}</div>
        {error && <p role="alert" className="error">{error}</p>}
    </div>
}
