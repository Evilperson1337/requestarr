import { useEffect, useId, useState } from 'react'
import { api } from '../api'
import { followLink } from '../navigation'
import { AuthScreen, MediaKinds } from '../components/AuthScreen'
import { LocalSignInForm } from '../components/LocalSignInForm'
import { LocalSignInPage } from './LocalSignInPage'

type Options = { oidcEnabled: boolean; providerName: string }

const about = 'Requestarr is a shared request and availability portal for movies, TV, music, games, and audiobooks; ask once, see availability, and get notified.'

// The signed-out screen (D040). It asks which ways in exist and fails towards the local
// form: if the answer is missing or wrong, the form is shown, never hidden.
export function SignInPage({ path, onSignedIn, error }: { path: string; onSignedIn: () => void; error?: string }) {
    // undefined: still asking; null: the request failed.
    const [options, setOptions] = useState<Options | null | undefined>(undefined)
    useEffect(() => {
        const controller = new AbortController()
        api<Options>('/auth/options', { cache: 'no-store', signal: controller.signal })
            .then(setOptions)
            .catch(() => { if (!controller.signal.aborted) setOptions(null) })
        return () => controller.abort()
    }, [])
    const provider = options?.providerName?.trim() || 'your organization account'
    const oidc = options?.oidcEnabled === true
    // The local form has its own address and never waits for the options.
    if (path === '/login') return <LocalSignInPage onSignedIn={onSignedIn} backToOptions={oidc} sso={oidc ? provider : undefined} />
    if (options === undefined) return <main className="shell narrowShell" aria-busy="true"><p role="status">Loading sign-in options…</p></main>
    if (oidc) return <AuthScreen>
        <h1>Sign in to <span className="authAccent">Requestarr</span></h1>
        <p className="authLead">{about}</p>
        <MediaKinds />
        {error && <p role="alert" className="error">{error}</p>}
        <a className="button authCta" href="/api/v1/auth/login">
            <svg width="26" height="26" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" focusable="false"><path d="M12 3l8 3v6c0 4.5-3.2 8-8 9-4.8-1-8-4.5-8-9V6z" /><path d="M8.5 12l2.5 2.5 4.5-5" /></svg>
            Sign in with {provider}
        </a>
        <p className="authOr">or</p>
        <PasswordDisclosure onSignedIn={onSignedIn} />
    </AuthScreen>
    return <LocalSignInPage onSignedIn={onSignedIn} heading="Sign in to Requestarr" intro={about}
        sso={options === null ? provider : undefined}
        notice={options === null ? <p role="status">Could not check which sign-in options are available, so the administrator form is shown.</p> : undefined} />
}

// The username and password option under single sign-on: closed until asked for, and the form
// is only rendered while open so its password field takes focus on opening.
function PasswordDisclosure({ onSignedIn }: { onSignedIn: () => void }) {
    const [open, setOpen] = useState(false)
    const id = useId()
    return <div className={open ? 'authDisclosure open' : 'authDisclosure'}>
        <button type="button" className="authDisclosureHead" aria-expanded={open} aria-controls={id} onClick={() => setOpen(o => !o)}>
            Username / Password
            <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" focusable="false"><path d="M6 9l6 6 6-6" /></svg>
        </button>
        <div id={id}>{open && <LocalSignInForm onSignedIn={onSignedIn} />}</div>
    </div>
}
