import { useState, type FormEvent } from 'react'
import { api, claimSetupAdmin, type Session } from '../api'

const MAX_ISSUER = 2048
const MAX_SUBJECT = 1024
const MIN_TOKEN = 32
const MAX_TOKEN = 512

export function SetupWizardPage({ session, onComplete }: { session: Session | null; onComplete: (session: Session) => void }) {
    const [issuer, setIssuer] = useState('')
    const [subject, setSubject] = useState('')
    const [token, setToken] = useState('')
    const [busy, setBusy] = useState(false)
    const [error, setError] = useState('')

    async function submit(event: FormEvent<HTMLFormElement>) {
        event.preventDefault()
        if (!session || busy) return
        setError('')
        setBusy(true)
        const submittedToken = token
        setToken('')
        try {
            await claimSetupAdmin({ issuer, subject, token: submittedToken }, session.csrfToken)
            const refreshed = await api<Session>('/auth/me', { cache: 'no-store' })
            onComplete(refreshed)
        } catch {
            setError('Admin claim could not be completed. Verify that you signed in as the configured identity and that the setup token is correct, then try again.')
        } finally {
            setToken('')
            setBusy(false)
        }
    }

    return <main className="shell setupShell"><section className="card setupCard">
        
        <h1>Connect sign-in, then claim the first admin</h1>
        <p>This page does not configure OIDC or edit server settings. OIDC must already be configured through the server environment before sign-in can work.</p>
        <div className="setupSteps">
            <h2>Before you continue</h2>
            <ol>
                <li>Configure the OIDC issuer, client ID, and client secret in the server environment, then restart Requestarr.</li>
                <li>Use the configured provider below to sign in as the exact identity that will become the first administrator.</li>
                <li>Run <code>requestarr setup-token --issuer ISSUER --subject SUBJECT</code> on the server. Enter the same exact issuer and subject here. The CLI writes the token to a protected file by default.</li>
            </ol>
        </div>
        {!session ? <><p>Setup is still required. Sign in through your configured identity provider to continue with the admin claim.</p><a className="button" href="/api/v1/auth/login">Sign in with OIDC</a></> : session.user.role === 'admin' ? <div role="status" className="setupNotice"><h2>Administrator access is ready</h2><p>Your session is already an administrator. Continue to Requestarr.</p><button className="button" onClick={() => onComplete(session)}>Continue to app</button></div> : <>
            <div className="setupNotice"><h2>Signed in, not yet an administrator</h2><p>Enter the identity values used when issuing the setup token. They are compared exactly with your verified sign-in identity. Requestarr does not reveal or prefill either value.</p></div>
            <form className="setupForm" onSubmit={submit}>
                <label>OIDC issuer
                    <input required maxLength={MAX_ISSUER} autoComplete="url" value={issuer} onChange={e => setIssuer(e.target.value)} />
                </label>
                <label>OIDC subject
                    <input required maxLength={MAX_SUBJECT} autoComplete="off" value={subject} onChange={e => setSubject(e.target.value)} />
                </label>
                <label>One-time setup token
                    <input required type="password" minLength={MIN_TOKEN} maxLength={MAX_TOKEN} autoComplete="new-password" value={token} onChange={e => setToken(e.target.value)} />
                </label>
                <p className="setupHint">The token is sent only in the protected claim request, is not saved in browser storage, and is cleared from this form when submitted.</p>
                {error && <p role="alert" className="errorBox">{error}</p>}
                <button className="button" disabled={busy}>{busy ? 'Claiming admin…' : 'Claim first admin'}</button>
            </form>
        </>}
        <p className="setupGuide">For operator instructions, see <a href="https://github.com/evilperson1337/requestarr/blob/main/docs/SETUP_WIZARD.md">the server setup guide</a>.</p>
    </section></main>
}
