import { useEffect, useState } from 'react'
import { api } from '../api'
import { followLink } from '../navigation'
import { LocalSignInPage } from './LocalSignInPage'

type Options = { oidcEnabled: boolean; providerName: string }

const about = 'Requestarr is a shared request and availability portal for movies, TV, music, games and audiobooks: ask for something once, see when it is available, and get told.'

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
    if (path === '/login') return <LocalSignInPage onSignedIn={onSignedIn} backToOptions={oidc} />
    if (options === undefined) return <main className="shell narrowShell" aria-busy="true"><p role="status">Loading sign-in options…</p></main>
    if (oidc) return <main className="shell narrowShell"><section className="card">
        <h1>Sign in to Requestarr</h1>
        <p>{about}</p>
        <p>Sign in with the account you use at your organization.</p>
        {error && <p role="alert" className="error">{error}</p>}
        <a className="button" href="/api/v1/auth/login">Sign in with {provider}</a>
        <p className="setupGuide"><a href="/login" onClick={followLink}>Administrator sign-in</a></p>
    </section></main>
    return <LocalSignInPage onSignedIn={onSignedIn} heading="Sign in to Requestarr" intro={about}
        notice={options === null ? <><p role="status">Could not check which sign-in options are available, so the administrator form is shown.</p><p className="setupGuide"><a href="/api/v1/auth/login">Sign in with {provider}</a></p></> : undefined} />
}
