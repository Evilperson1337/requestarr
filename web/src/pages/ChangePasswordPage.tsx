import { useState, type FormEvent } from 'react'
import { api, ApiError, type Session } from '../api'

const MIN = 12
const MAX = 128

// Shown to the local administrator until they choose their own password (D040). The server
// refuses every other route meanwhile, so this is the only screen.
export function ChangePasswordPage({ session, onChanged, onSignOut }: { session: Session; onChanged: () => void; onSignOut: () => void }) {
    const [current, setCurrent] = useState('')
    const [next, setNext] = useState('')
    const [confirm, setConfirm] = useState('')
    const [busy, setBusy] = useState(false)
    const [error, setError] = useState('')

    async function submit(event: FormEvent<HTMLFormElement>) {
        event.preventDefault()
        if (busy) return
        const length = [...next].length
        if (length < MIN || length > MAX) { setError(`Choose a password of ${MIN} to ${MAX} characters. A passphrase works well.`); return }
        if (next !== confirm) { setError('The new password and its confirmation do not match.'); return }
        if (next === current) { setError('Choose a password different from the current one.'); return }
        setError('')
        setBusy(true)
        try {
            await api<void>('/auth/local/password', { method: 'POST', body: JSON.stringify({ currentPassword: current, newPassword: next }), cache: 'no-store' }, session.csrfToken)
            setCurrent(''); setNext(''); setConfirm('')
            onChanged()
        } catch (e) {
            if (e instanceof ApiError && e.status === 429) setError(`Too many attempts. Try again in ${e.retryAfter || 'a few'} seconds.`)
            else setError(e instanceof Error ? e.message : 'Could not change the password. Try again.')
        } finally { setBusy(false) }
    }

    return <main className="shell setupShell"><section className="card setupCard">
        <h1>Choose a new password</h1>
        <p>You signed in with a one-time password. Choose your own before continuing: {MIN} to {MAX} characters, no other rules. Other sessions of this account end when you change it.</p>
        <form className="setupForm" onSubmit={submit}>
            <label>Current password
                <input required type="password" maxLength={1024} autoComplete="current-password" autoFocus value={current} onChange={e => setCurrent(e.target.value)} />
            </label>
            <label>New password
                <input required type="password" autoComplete="new-password" value={next} onChange={e => setNext(e.target.value)} />
            </label>
            <label>Confirm new password
                <input required type="password" autoComplete="new-password" value={confirm} onChange={e => setConfirm(e.target.value)} />
            </label>
            {error && <p role="alert" className="errorBox">{error}</p>}
            <button className="button" disabled={busy}>{busy ? 'Changing…' : 'Change password'}</button>
        </form>
        <p className="setupGuide"><button type="button" className="button secondary" onClick={onSignOut}>Sign out</button></p>
    </section></main>
}
