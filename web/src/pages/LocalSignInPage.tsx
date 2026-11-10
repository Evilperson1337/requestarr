import type { ReactNode } from 'react'
import { AuthScreen } from '../components/AuthScreen'
import { LocalSignInForm } from '../components/LocalSignInForm'
import { followLink } from '../navigation'

// The local administrator's sign-in (D040). It has its own address, /login, so a broken or
// missing identity-provider setting can never hide it.
export function LocalSignInPage({ onSignedIn, heading = 'Administrator sign-in', intro, notice, backToOptions = false, sso }: { onSignedIn: () => void; heading?: string; intro?: string; notice?: ReactNode; backToOptions?: boolean; sso?: string }) {
    return <AuthScreen wide>
        <h1>{heading}</h1>
        {intro && <p className="authLead">{intro}</p>}
        {notice}
        <p className="authLead">Sign in as the local administrator. Everyone else signs in through the organization account.</p>
        <LocalSignInForm onSignedIn={onSignedIn} />
        {sso && <a className="button secondary authSso" href="/api/v1/auth/login">Sign in with {sso}</a>}
        {backToOptions && <p className="authAlt"><a href="/" onClick={followLink}>Back to the sign-in options</a></p>}
    </AuthScreen>
}
