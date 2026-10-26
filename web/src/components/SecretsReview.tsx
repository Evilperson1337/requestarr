import { useCallback, useEffect, useState } from 'react'
import { api, type SecretsOverview } from '../api'

// Review-step summary of stored secrets (D024): shadowed and unreadable values
// are warned about, and secrets saved but not yet in effect are listed.
// Stored values are never deleted automatically; clearing is an explicit action.
export function SecretsReview({ csrfToken }: { csrfToken: string }) {
    const [overview, setOverview] = useState<SecretsOverview | null>(null)
    const [error, setError] = useState('')
    const [busy, setBusy] = useState(false)

    const refresh = useCallback(async () => {
        try {
            const result = await api<SecretsOverview>('/admin/secrets', { cache: 'no-store' })
            if (!result || !Array.isArray(result.secrets)) throw new Error('unexpected response')
            setOverview(result); setError('')
        } catch {
            setOverview(null); setError('Stored secret status could not be loaded.')
        }
    }, [])
    useEffect(() => { void refresh() }, [refresh])

    async function clear(name: string, label: string) {
        if (busy) return
        setBusy(true); setError('')
        try {
            await api<void>(`/admin/secrets/${name}`, { method: 'DELETE' }, csrfToken)
            await refresh()
        } catch (e) {
            setError(e instanceof Error ? e.message : `${label} could not be cleared.`)
        } finally { setBusy(false) }
    }

    const secrets = overview?.secrets ?? []
    const shadowed = secrets.filter(s => s.source === 'stored (shadowed by environment)')
    const unreadable = secrets.filter(s => s.unreadable)
    const pending = secrets.filter(s => s.pendingRestart)
    return <section aria-label="Stored secrets review">
        <h3>Stored secrets</h3>
        {error && <p role="alert" className="error">{error}</p>}
        {shadowed.map(s => <div key={s.name} role="status" className="errorBox">
            <p>{s.label}: the stored value is shadowed by the environment variable <code>{s.envVar}</code> and is not used. If that variable is removed, the stored value takes effect after a restart.</p>
            <button className="button secondary" type="button" disabled={busy} aria-label={`Clear stored ${s.label}`} onClick={() => void clear(s.name, s.label)}>Clear stored value</button>
        </div>)}
        {unreadable.map(s => <div key={s.name} role="alert" className="errorBox"><p>{s.label}: stored secret unreadable. Enter it again or restore <code>REQUESTARR_SECRETS_KEY</code>.</p></div>)}
        {pending.length > 0 && <>
            <p>These changes apply after Requestarr restarts.</p>
            <ul aria-label="Saved, pending restart">{pending.map(s => <li key={s.name}>{s.label}</li>)}</ul>
        </>}
        {overview && !shadowed.length && !unreadable.length && !pending.length && <p>{secrets.some(s => s.configured) ? 'Stored secrets are in effect.' : 'No secrets are stored.'}</p>}
    </section>
}
