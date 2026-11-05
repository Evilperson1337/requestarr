import { useEffect, useId, useState } from 'react'
import { api, ApiError, type IgdbSettings } from '../api'

// The non-secret half of the IGDB credentials. The secret is the stored-secret field
// beside it; an environment value for the ID wins and is shown read-only.
export function IgdbClientId({ csrfToken, onRequestError, onChanged }: { csrfToken: string; onRequestError?: (error: unknown) => void; onChanged?: () => void }) {
    const id = useId()
    const [saved, setSaved] = useState<IgdbSettings | null>(null)
    const [value, setValue] = useState('')
    const [busy, setBusy] = useState(false)
    const [error, setError] = useState('')
    const [notice, setNotice] = useState('')
    const [loadFailed, setLoadFailed] = useState(false)

    useEffect(() => {
        const controller = new AbortController()
        api<IgdbSettings>('/admin/settings/igdb', { cache: 'no-store', signal: controller.signal }).then(result => {
            if (controller.signal.aborted) return
            setSaved({ clientId: result.clientId ?? '', fromEnvironment: result.fromEnvironment === true })
            setValue(result.clientId ?? '')
        }).catch(e => { if (!controller.signal.aborted) { onRequestError?.(e); setLoadFailed(true) } })
        return () => controller.abort()
    }, [onRequestError])

    async function save() {
        if (busy || !saved) return
        setBusy(true); setError(''); setNotice('')
        try {
            const result = await api<IgdbSettings>('/admin/settings/igdb', { method: 'PUT', body: JSON.stringify({ clientId: value.trim() }) }, csrfToken)
            setSaved({ clientId: result.clientId, fromEnvironment: false }); setValue(result.clientId)
            setNotice('IGDB client ID saved and applied.'); onChanged?.()
        } catch (e) {
            onRequestError?.(e)
            setError(e instanceof ApiError && e.status === 409 ? 'The client ID is set by the server environment and cannot be changed here.'
                : e instanceof ApiError && e.code === 'settings_not_applied' ? 'The IGDB client ID was saved but could not be applied. The previous configuration is still in use.'
                    : 'The IGDB client ID could not be saved.')
        } finally { setBusy(false) }
    }

    const fromEnvironment = saved?.fromEnvironment === true
    return <div className="secretField">
        <label htmlFor={id}>IGDB client ID</label>
        <input id={id} type="text" autoComplete="off" autoCorrect="off" autoCapitalize="off" spellCheck={false} maxLength={256}
            value={value} disabled={!saved || fromEnvironment || busy} aria-describedby={`${id}-help`}
            onChange={event => setValue(event.target.value)}
            onKeyDown={event => { if (event.key === 'Enter') { event.preventDefault(); void save() } }} />
        <div className="draftActions">
            <button className="button secondary" type="button" disabled={!saved || fromEnvironment || busy || value.trim() === saved.clientId} onClick={() => void save()}>Save IGDB client ID</button>
        </div>
        <p id={`${id}-help`}>
            {loadFailed ? 'The client ID could not be loaded.'
                : fromEnvironment ? <>Set by <code>REQUESTARR_IGDB_CLIENT_ID</code> on the server, which always wins. Change it there.</>
                    : <>The Twitch application client ID. It is not secret, so it is shown here, and it applies at once.</>}
        </p>
        <div aria-live="polite">{notice && <p role="status">{notice}</p>}</div>
        {error && <p role="alert" className="error">{error}</p>}
    </div>
}
