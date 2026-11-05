import { useState, type FormEvent, type ReactNode } from 'react'
import { api, ApiError } from '../api'
import { followLink, navigate } from '../navigation'

// The local administrator's sign-in (D040). It has its own address, /login, so a broken or
// missing identity-provider setting can never hide it.
export function LocalSignInPage({ onSignedIn, heading = 'Administrator sign-in', intro, notice, backToOptions = false }: { onSignedIn: () => void; heading?: string; intro?: string; notice?: ReactNode; backToOptions?: boolean }) {
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

    return <main className="shell setupShell"><section className="card setupCard">
        <h1>{heading}</h1>
        {intro && <p>{intro}</p>}
        {notice}
        <p>Sign in as the local administrator. Everyone else signs in through the organization account.</p>
        <form className="setupForm" onSubmit={submit}>
            <label>Username
                <input required maxLength={64} autoComplete="username" autoCapitalize="none" spellCheck={false} value={username} onChange={e => setUsername(e.target.value)} />
            </label>
            <label>Password
                <input required type="password" maxLength={1024} autoComplete="current-password" autoFocus value={password} onChange={e => setPassword(e.target.value)} />
            </label>
            {error && <p role="alert" className="errorBox">{error}</p>}
            <button className="button" disabled={busy}>{busy ? 'Signing in…' : 'Sign in'}</button>
        </form>
        {backToOptions && <p className="setupGuide"><a href="/" onClick={followLink}>Back to the sign-in options</a></p>}
    </section></main>
}
