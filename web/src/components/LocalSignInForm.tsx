import { useState, type FormEvent } from 'react'
import { api, ApiError } from '../api'
import { navigate } from '../navigation'

// The local administrator's username and password form (D040), shared by the /login page and the
// disclosure on the sign-in screen. The password field takes focus when the form appears.
export function LocalSignInForm({ onSignedIn }: { onSignedIn: () => void }) {
    const [username, setUsername] = useState('admin')
    const [password, setPassword] = useState('')
    const [busy, setBusy] = useState(false)
    const [error, setError] = useState('')

    async function submit(event: FormEvent<HTMLFormElement>) {
        event.preventDefault()
        if (busy) return
        setError('')
        setBusy(true)
        const attempt = password
        setPassword('')
        try {
            await api<unknown>('/auth/local/login', { method: 'POST', body: JSON.stringify({ username, password: attempt }), cache: 'no-store' })
            navigate('/', {}, true)
            onSignedIn()
        } catch (e) {
            if (e instanceof ApiError && e.status === 429) setError(`Too many attempts. Try again in ${e.retryAfter || 'a few'} seconds.`)
            else if (e instanceof ApiError && e.status === 401) setError('Incorrect username or password.')
            else setError(e instanceof Error ? e.message : 'Could not sign in. Try again.')
        } finally { setBusy(false) }
    }

    return <form className="setupForm" onSubmit={submit}>
        <label>Username
            <input required maxLength={64} autoComplete="username" autoCapitalize="none" spellCheck={false} value={username} onChange={e => setUsername(e.target.value)} />
        </label>
        <label>Password
            <input required type="password" maxLength={1024} autoComplete="current-password" autoFocus value={password} onChange={e => setPassword(e.target.value)} />
        </label>
        {error && <p role="alert" className="errorBox">{error}</p>}
        <button className="button" disabled={busy}>{busy ? 'Signing in…' : 'Sign in'}</button>
    </form>
}
