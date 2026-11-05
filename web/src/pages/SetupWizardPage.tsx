import { useState, type FormEvent } from 'react'
import { api, claimSetupAdmin, type FirstAdminClaim, type Session } from '../api'

const MIN_TOKEN = 32
const MAX_TOKEN = 512

// POSIX single-quote escaping, so a subject with a quote still pastes safely.
const shellQuote = (value: string) => `'${value.replace(/'/g, `'\\''`)}'`

export function setupTokenCommand(claim: FirstAdminClaim) {
    return `docker exec <container-name> /requestarr setup-token${claim.issuerIsConfigured ? '' : ` --issuer ${shellQuote(claim.issuer)}`} --subject ${shellQuote(claim.subject)} --stdout`
}

export function SetupWizardPage({ session, onComplete }: { session: Session | null; onComplete: (session: Session) => void }) {
    const [token, setToken] = useState('')
    const [busy, setBusy] = useState(false)
    const [error, setError] = useState('')
    const [copied, setCopied] = useState(false)

    async function submit(event: FormEvent<HTMLFormElement>) {
        event.preventDefault()
        if (!session || busy) return
        setError('')
        setBusy(true)
        const submittedToken = token
        setToken('')
        try {
            await claimSetupAdmin({ token: submittedToken }, session.csrfToken)
            const refreshed = await api<Session>('/auth/me', { cache: 'no-store' })
            onComplete(refreshed)
        } catch {
            setError('Admin claim could not be completed. Check that the setup token is correct and has not expired or been used, then try again.')
        } finally {
            setToken('')
            setBusy(false)
        }
    }

    async function copy(command: string) {
        try { await navigator.clipboard.writeText(command); setCopied(true) } catch { setCopied(false) }
    }

    return <main className="shell setupShell"><section className="card setupCard">
        
        <h1>Connect sign-in, then claim the first admin</h1>
        <p>This page does not configure OIDC or edit server settings. OIDC must already be configured through the server environment before sign-in can work.</p>
        <div className="setupSteps">
            <h2>Before you continue</h2>
            <ol>
                <li>Configure the OIDC issuer, client ID, and client secret in the server environment, then restart Requestarr.</li>
                <li>Sign in below as the person who will become the first administrator.</li>
                <li>This page then shows the exact command to run on the server. Run it, and paste the token it prints.</li>
            </ol>
        </div>
        {!session ? <><p>Setup is still required. Sign in through your configured identity provider to continue with the admin claim.</p><a className="button" href="/api/v1/auth/login">Sign in with OIDC</a></> : session.user.role === 'admin' ? <div role="status" className="setupNotice"><h2>Administrator access is ready</h2><p>Your session is already an administrator. Continue to Requestarr.</p><button className="button" onClick={() => onComplete(session)}>Continue to app</button></div> : <>
            <div className="setupNotice"><h2>Signed in, not yet an administrator</h2>{session.firstAdminClaim ? <><p>Run this on the server that hosts Requestarr. It prints a one-time token for your sign-in identity, valid for 30 minutes. Replace <code>&lt;container-name&gt;</code> with the name of the Requestarr container.</p>
                <pre className="setupCommand"><code data-testid="setup-token-command">{setupTokenCommand(session.firstAdminClaim)}</code></pre>
                <button type="button" className="button secondary" onClick={() => void copy(setupTokenCommand(session.firstAdminClaim!))}>Copy command</button>{copied && <span role="status"> Copied.</span>}</> : <p>Requestarr could not show the command for your sign-in. Run <code>requestarr setup-token --subject YOUR_OIDC_USER_ID --stdout</code> on the server, where the subject is your identity provider's user ID, and paste the token below.</p>}</div>
            <form className="setupForm" onSubmit={submit}>
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
