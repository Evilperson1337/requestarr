import { useCallback, useId, useRef, useState, type FormEvent } from 'react'
import { api, type AuthenticationState, type Session } from '../api'
import { IntegrationCard, type Tone } from './IntegrationCard'
import { LoadingCard, useBusy, useLoadedResource } from './authCard'
import { LocalAdministratorCard } from './LocalAdministratorCard'

const endpoint = '/admin/settings/authentication'
type Busy = 'test' | 'save' | 'enable' | 'discard'
const loadOidc = (signal?: AbortSignal) => api<AuthenticationState>(endpoint, { cache: 'no-store', signal })

// Settings, Authentication (D040): how people sign in. Secrets are write-only: a typed
// client secret lives in one input until it is submitted, is cleared whether or not the
// request works, and is never put in browser storage or kept in page state.
export function AuthenticationSettings({ session, onRequestError }: { session: Session; onRequestError: (error: unknown) => void }) {
    return <section className="integrationGroup" aria-labelledby="group-authentication">
        <div className="groupHead"><h2 id="group-authentication">Authentication</h2><p>How people sign in. Everyone except the local administrator signs in through your OpenID Connect provider. Changes apply at once, without a restart.</p></div>
        <ul className="integrations authCards">
            <OidcCard csrfToken={session.csrfToken} onRequestError={onRequestError} />
            <LocalAdministratorCard csrfToken={session.csrfToken} canChangePassword={session.localAdministrator === true} onRequestError={onRequestError} />
        </ul>
    </section>
}

function describe(state: AuthenticationState): { label: string; line: string; tone: Tone } {
    const name = state.effective.providerName || 'your provider'
    switch (state.state) {
        case 'not_configured': return { label: 'Not configured', line: 'OIDC is not configured. Everyone signs in with the local administrator.', tone: 'off' }
        case 'environment_incomplete': return { label: 'Incomplete', line: 'OIDC is not active: the environment configuration is incomplete.', tone: 'warn' }
    }
    if (state.working) return { label: state.readOnly ? 'On · set by environment' : `On · ${name}`, line: 'OIDC is on and a provider is active. This shows the last discovery, not a live check.', tone: 'on' }
    if (state.effective.enabled) return { label: 'On, not active', line: 'OIDC is on but no provider is active, so people cannot sign in through it.', tone: 'warn' }
    return { label: 'Off', line: 'OIDC is off. Saved settings are kept.', tone: 'off' }
}

function OidcCard({ csrfToken, onRequestError }: { csrfToken: string; onRequestError: (error: unknown) => void }) {
    const id = useId()
    const [providerName, setProviderName] = useState('')
    const [issuer, setIssuer] = useState('')
    const [clientId, setClientId] = useState('')
    const [secret, setSecret] = useState('')
    const { busy, error, setError, notice, setNotice, run: runBusy } = useBusy<Busy>(onRequestError)
    const [copied, setCopied] = useState('')
    const line = useRef<HTMLParagraphElement>(null)

    const fillFields = useCallback((next: AuthenticationState) => {
        setProviderName(next.effective.providerName); setIssuer(next.effective.issuer); setClientId(next.effective.clientId)
    }, [])
    const { value: state, setValue: setState, loadError, retry } = useLoadedResource(loadOidc, onRequestError, 'Could not load the OpenID Connect settings.', fillFields)
    const adopt = (next: AuthenticationState) => { setState(next); fillFields(next) }

    if (!state) return <LoadingCard title="OpenID Connect" error={loadError} onRetry={retry} />

    const locked = state.readOnly
    const { label, line: stateLine, tone } = describe(state)
    const typedSecret = secret !== ''
    const dirty = providerName !== state.effective.providerName || issuer !== state.effective.issuer || clientId !== state.effective.clientId || typedSecret
    const previous = state.saved || locked ? 'Nothing was changed. The previous configuration is still in use.' : 'Nothing was saved.'

    function draft() {
        const body = { enabled: state!.effective.enabled, providerName: providerName.trim(), issuer: issuer.trim(), clientId: clientId.trim(), ...(typedSecret ? { clientSecret: secret } : {}) }
        if (locked) return body
        if (!body.providerName || !body.issuer || !body.clientId) { setError('Enter a provider name, an issuer and a client ID.'); return null }
        if (!typedSecret && !state!.secretConfigured) { setError('Enter the client secret from your provider.'); return null }
        return body
    }
    const run = (kind: Busy, work: () => Promise<void>) => runBusy(kind, work, message => `${message} ${kind === 'enable' ? 'The setting was not changed.' : previous}`)
    function clearSecret() { setSecret('') } // never keep a secret in the page after submit
    function test() {
        const body = draft()
        if (!body) return
        const hadSecret = typedSecret
        clearSecret()
        void run('test', async () => {
            await api<{ ok: true }>(`${endpoint}/test`, { method: 'POST', body: JSON.stringify(body), cache: 'no-store' }, csrfToken)
            setNotice(`Discovery passed. The provider's metadata could be read. This does not check the client secret or a full sign-in.${hadSecret ? ' The client secret field was cleared; enter it again before saving.' : ''}`)
        })
    }
    function save(event: FormEvent) {
        event.preventDefault()
        if (locked || !dirty) return
        const body = draft()
        if (!body) return
        clearSecret()
        void run('save', async () => {
            adopt(await api<AuthenticationState>(endpoint, { method: 'PUT', body: JSON.stringify(body), cache: 'no-store' }, csrfToken))
            setNotice('OIDC settings saved.')
        })
    }
    function toggle(enabled: boolean) {
        void run('enable', async () => {
            const next = await api<AuthenticationState>(`${endpoint}/enabled`, { method: 'PUT', body: JSON.stringify({ enabled }), cache: 'no-store' }, csrfToken)
            setState(next)
            setNotice(enabled ? 'OIDC turned on.' : 'OIDC turned off. Saved settings are kept.')
        })
    }
    function discard() {
        void run('discard', async () => {
            adopt(await api<AuthenticationState>(endpoint, { method: 'DELETE', cache: 'no-store' }, csrfToken))
            setNotice('Saved settings discarded. The environment settings are unchanged.')
            line.current?.focus()
        })
    }
    async function copyRedirect() {
        try { await navigator.clipboard.writeText(state!.redirectUri); setCopied('Redirect address copied.') }
        catch { setCopied('Could not copy. Select the redirect address and copy it.') }
    }
    const keyHelp = `${id}-key`, secretHelp = `${id}-secret`
    const secretText = locked ? (state.missing.includes('REQUESTARR_OIDC_CLIENT_SECRET') ? 'Not set in the environment.' : 'Set by environment.') : `Stored encrypted, never shown again. ${state.secretConfigured ? 'A client secret is saved; leave this blank to keep it.' : 'No client secret is saved yet.'}`
    const missingKey = !state.canSaveSecret && !locked

    return <IntegrationCard slug="oidc" name="OpenID Connect" status={label} tone={tone} meta={<span ref={line} tabIndex={-1}>{stateLine}</span>} defaultOpen
        actions={<label className="switch"><input type="checkbox" role="switch" checked={state.effective.enabled} disabled={locked || !state.saved} onChange={event => toggle(event.target.checked)} /><span>Enable OIDC</span></label>}>
        <div aria-live="polite" className="authMessages">
            {locked && <p className="authNotice"><strong>Set by environment.</strong> These values come from REQUESTARR_OIDC_* variables and cannot be changed here. Change them in your deployment and restart Requestarr.</p>}
            {state.ignored.length > 0 && <div className="authNotice warn">
                <p><strong>Saved settings are ignored.</strong> Environment variables are set, so they replace the saved settings as one unit.</p>
                <p>Ignored: {state.ignored.join(', ')}.</p>
                <p>{state.differing.length ? `Differs from the environment: ${state.differing.join(', ')}.` : 'The saved values are the same values as the environment, but they are still ignored.'}</p>
                <button type="button" className="button secondary" disabled={!!busy} onClick={discard}>Discard saved settings</button>
            </div>}
            {state.missing.length > 0 && <div role="alert" className="errorBox"><p>The environment configuration is incomplete, and saved settings cannot fill the gaps. Set:</p><ul>{state.missing.map(name => <li key={name}><code>{name}</code></li>)}</ul></div>}
            {missingKey && <p id={keyHelp} className="authNotice warn">REQUESTARR_SECRETS_KEY is not configured, so a client secret cannot be stored. Set the master key in your deployment. The local administrator can still sign in.</p>}
            {state.secretUnreadable && <p role="alert" className="errorBox">The saved client secret cannot be read. Restore REQUESTARR_SECRETS_KEY, or enter a new client secret and save.</p>}
            {state.problem && <p role="alert" className="errorBox">{state.problem}</p>}
            {error && <p role="alert" className="errorBox">{error}</p>}
            {notice && <p role="status" className="authOk">{notice}</p>}
        </div>
        <form className="authForm" onSubmit={save} noValidate>
            <div className="authFields">
                <div className="field"><label htmlFor={`${id}-name`}>Provider name</label><input id={`${id}-name`} value={providerName} readOnly={locked} maxLength={100} autoComplete="off" onChange={e => setProviderName(e.target.value)} /><p className="fieldHelp">Shown on the sign-in button.</p></div>
                <div className="field"><label htmlFor={`${id}-client`}>Client ID</label><input id={`${id}-client`} value={clientId} readOnly={locked} maxLength={512} autoComplete="off" spellCheck={false} onChange={e => setClientId(e.target.value)} /></div>
                <div className="field wide"><label htmlFor={`${id}-issuer`}>Issuer</label><input id={`${id}-issuer`} type="url" inputMode="url" placeholder="https://" value={issuer} readOnly={locked} maxLength={2048} autoComplete="off" spellCheck={false} onChange={e => setIssuer(e.target.value)} /><p className="fieldHelp">The address of your provider, such as a Keycloak realm.</p></div>
                <div className="field wide"><label htmlFor={`${id}-secret`}>Client secret</label><input id={`${id}-secret`} type="password" value={secret} disabled={locked || !state.canSaveSecret} maxLength={4096} autoComplete="new-password" aria-describedby={`${secretHelp}${missingKey ? ` ${keyHelp}` : ''}`} onChange={e => setSecret(e.target.value)} /><p id={secretHelp} className="fieldHelp">{secretText}</p></div>
            </div>
            <div className="field"><label htmlFor={`${id}-redirect`}>Redirect address</label>
                <div className="authCopy"><input id={`${id}-redirect`} readOnly value={state.redirectUri} aria-describedby={`${id}-redirect-help`} onFocus={e => e.currentTarget.select()} /><button className="button secondary" type="button" onClick={() => void copyRedirect()}>Copy redirect address</button></div>
                <p id={`${id}-redirect-help`} className="fieldHelp">Add this to your provider as a valid redirect URI.</p>
                <p role="status" className="authOk">{copied}</p></div>
            <div className="draftActions">
                {!locked && <button className="button" type="submit" disabled={!dirty || !!busy || !state.canSaveSecret}>{busy === 'save' ? 'Saving…' : 'Save OIDC settings'}</button>}
                <button className="button secondary" type="button" disabled={!!busy} onClick={test}>{busy === 'test' ? 'Testing…' : 'Test OIDC connection'}</button>
                {!state.saved && !locked && <span className="fieldHelp">Save the settings, then switch OIDC on.</span>}
            </div>
        </form>
    </IntegrationCard>
}
