import { useCallback, useEffect, useId, useRef, useState, type FormEvent } from 'react'
import { api, ApiError, type CreatedLocalAdministrator, type LocalAdministratorState } from '../api'
import { IntegrationCard, type Tone } from './IntegrationCard'

const endpoint = '/admin/settings/authentication/local-administrator'
const MIN = 12
const MAX = 128

// The recovery account (D040). Its generated password is held in this component only
// until the administrator confirms they saved it: never in storage, a URL or a log, and
// no API call returns it again.
export function LocalAdministratorCard({ csrfToken, onRequestError }: { csrfToken: string; onRequestError: (error: unknown) => void }) {
    const id = useId()
    const [admin, setAdmin] = useState<LocalAdministratorState | null>(null)
    const [loadError, setLoadError] = useState('')
    const [retry, setRetry] = useState(0)
    const [busy, setBusy] = useState('')
    const [error, setError] = useState('')
    const [notice, setNotice] = useState('')
    const [created, setCreated] = useState<CreatedLocalAdministrator | null>(null)
    const [copied, setCopied] = useState('')
    const reveal = useRef<HTMLDivElement>(null)
    const [current, setCurrent] = useState('')
    const [next, setNext] = useState('')
    const [confirm, setConfirm] = useState('')
    const [formError, setFormError] = useState('')
    const [formNotice, setFormNotice] = useState('')

    const load = useCallback((signal?: AbortSignal) => api<LocalAdministratorState>(endpoint, { cache: 'no-store', signal }), [])
    useEffect(() => {
        const controller = new AbortController()
        setLoadError('')
        load(controller.signal).then(result => { if (!controller.signal.aborted) setAdmin(result) })
            .catch(e => { if (!controller.signal.aborted) { onRequestError(e); setLoadError('Could not load the local administrator.') } })
        return () => controller.abort()
    }, [load, onRequestError, retry])
    useEffect(() => { if (created) reveal.current?.focus() }, [created])

    if (!admin) return <li className="integration" aria-labelledby={`${id}-load`}>
        <h3 id={`${id}-load`} className="authLoadingTitle">Local administrator</h3>
        {loadError ? <div role="alert" className="errorBox"><p>{loadError}</p><button type="button" className="button secondary" onClick={() => setRetry(n => n + 1)}>Try again</button></div> : <p role="status">Loading…</p>}
    </li>

    async function run(kind: string, work: () => Promise<void>) {
        if (busy) return
        setBusy(kind); setError(''); setNotice('')
        try { await work() } catch (e) { onRequestError(e); setError(e instanceof Error && e.message ? e.message : 'The request failed.') } finally { setBusy('') }
    }
    function create() {
        void run('create', async () => {
            setCreated(await api<CreatedLocalAdministrator>(endpoint, { method: 'POST', cache: 'no-store' }, csrfToken))
            setAdmin(await load())
        })
    }
    function setDisabled(disabled: boolean) {
        void run('disable', async () => {
            setAdmin(await api<LocalAdministratorState>(`${endpoint}/disabled`, { method: 'PUT', body: JSON.stringify({ disabled }), cache: 'no-store' }, csrfToken))
            setNotice(disabled ? 'Local administrator disabled. Its sessions have ended.' : 'Local administrator enabled.')
        })
    }
    async function copyPassword() {
        try { await navigator.clipboard.writeText(created!.password); setCopied('Password copied.') } catch { setCopied('Could not copy. Select the password and copy it.') }
    }
    async function changePassword(event: FormEvent<HTMLFormElement>) {
        event.preventDefault()
        if (busy) return
        setFormNotice('')
        const length = [...next].length
        if (!current) { setFormError('Enter your current password.'); return }
        if (length < MIN || length > MAX) { setFormError(`Choose a new password of ${MIN} to ${MAX} characters. A passphrase works well.`); return }
        if (next !== confirm) { setFormError('The new password and its confirmation do not match.'); return }
        if (next === current) { setFormError('Choose a password different from the current one.'); return }
        setFormError(''); setBusy('password')
        const body = JSON.stringify({ currentPassword: current, newPassword: next })
        setCurrent(''); setNext(''); setConfirm('') // never keep a password in the page after submit
        try {
            await api<void>('/auth/local/password', { method: 'POST', body, cache: 'no-store' }, csrfToken)
            setFormNotice('Password changed. Other sessions of this account were signed out.')
        } catch (e) {
            // These two 403s are answers about this form, not a lost administrator session.
            if (!(e instanceof ApiError && (e.code === 'current_password_incorrect' || e.code === 'forbidden'))) onRequestError(e)
            if (e instanceof ApiError && e.code === 'current_password_incorrect') setFormError('The current password is not correct.')
            else if (e instanceof ApiError && e.code === 'forbidden') setFormError('Only the local administrator can change this password. You are signed in with another account.')
            else if (e instanceof ApiError && e.status === 429) setFormError(`Too many attempts. Try again in ${e.retryAfter || 'a few'} seconds.`)
            else setFormError(e instanceof Error ? e.message : 'Could not change the password. Try again.')
        } finally { setBusy('') }
    }

    const status: { label: string; tone: Tone } = !admin.exists ? { label: 'Not created', tone: 'off' } : admin.disabled ? { label: 'Disabled', tone: 'warn' } : { label: 'Enabled', tone: 'on' }
    const meta = !admin.exists
        ? 'No local administrator on this instance. Create one to keep a recovery sign-in that does not depend on your identity provider.'
        : admin.disabled ? 'The account is disabled, so it cannot sign in. Its password is kept.'
            : admin.canDisable ? 'The recovery account, username admin. Disabling it ends its sessions and keeps its password.'
                : 'The recovery account, username admin. Disabling needs another enabled administrator: give someone the Admin role under Users first.'
    const action = !admin.exists
        ? <button type="button" className="button" disabled={!!busy} onClick={create}>{busy === 'create' ? 'Creating…' : 'Create local administrator'}</button>
        : <button type="button" className="button secondary" disabled={!!busy} aria-label={`${admin.disabled ? 'Enable' : 'Disable'} local administrator`} onClick={() => setDisabled(!admin.disabled)}>{admin.disabled ? 'Enable' : 'Disable'}</button>

    return <IntegrationCard slug="local" name="Local administrator" status={status.label} tone={status.tone} meta={meta} actions={action} configurable={admin.exists || !!created || !!error || !!notice} defaultOpen>
        <div aria-live="polite" className="authMessages">
            {error && <p role="alert" className="errorBox">{error}</p>}
            {notice && <p role="status" className="authOk">{notice}</p>}
        </div>
        {created && <div ref={reveal} role="group" tabIndex={-1} aria-labelledby={`${id}-once`} className="authNotice reveal">
            <h4 id={`${id}-once`}>One-time password</h4>
            <p>Username <code>{created.username}</code>. Password:</p>
            <p><code className="oneTime">{created.password}</code></p>
            <p>This is shown once and cannot be shown again. Sign in with it; you will be asked to choose your own password straight away.</p>
            <div className="draftActions">
                <button type="button" className="button secondary" onClick={() => void copyPassword()}>Copy password</button>
                <button type="button" className="button" onClick={() => { setCreated(null); setCopied('') }}>I have saved the password</button>
            </div>
            <p role="status" className="authOk">{copied}</p>
        </div>}
        {admin.exists && <form className="authForm" onSubmit={e => void changePassword(e)} noValidate>
            <h4>Change password</h4>
            <p className="fieldHelp">For the signed-in local administrator. {MIN} to {MAX} characters, no other rules. Other sessions of this account end when you change it.</p>
            <div className="authFields">
                <div className="field wide"><label htmlFor={`${id}-current`}>Current password</label><input id={`${id}-current`} type="password" autoComplete="current-password" maxLength={1024} value={current} onChange={e => setCurrent(e.target.value)} /></div>
                <div className="field"><label htmlFor={`${id}-new`}>New password</label><input id={`${id}-new`} type="password" autoComplete="new-password" value={next} onChange={e => setNext(e.target.value)} /></div>
                <div className="field"><label htmlFor={`${id}-confirm`}>Confirm new password</label><input id={`${id}-confirm`} type="password" autoComplete="new-password" value={confirm} onChange={e => setConfirm(e.target.value)} /></div>
            </div>
            <div aria-live="polite" className="authMessages">
                {formError && <p role="alert" className="errorBox">{formError}</p>}
                {formNotice && <p role="status" className="authOk">{formNotice}</p>}
            </div>
            <div className="draftActions"><button className="button" type="submit" disabled={!!busy}>{busy === 'password' ? 'Changing…' : 'Change password'}</button></div>
        </form>}
    </IntegrationCard>
}
