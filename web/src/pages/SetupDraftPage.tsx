import { useEffect, useRef, useState, type FormEvent } from 'react'
import { SecretField } from '../components/SecretField'
import { api, ApiError, type Session, type SetupDraft, type SetupSettings, type SetupStep, type SetupTarget, type SetupSource } from '../api'

const steps: { key: SetupStep; label: string }[] = [
    { key: 'general', label: 'General' }, { key: 'authentication', label: 'Authentication' },
    { key: 'providers', label: 'Providers' }, { key: 'libraries', label: 'Libraries' },
    { key: 'notifications', label: 'Notifications' }, { key: 'policy', label: 'Request Policy' }, { key: 'review', label: 'Review' },
]

const blockerReason = { not_checked: 'not checked yet', failed: 'last check failed', expired: 'check expired' } as const
const mediaLabels = ['movie', 'tv', 'album', 'song', 'game'] as const

export function SetupDraftPage({ session, onFinished }: { session: Session; onFinished?: () => void }) {
    const [draft, setDraft] = useState<SetupDraft | null>(null)
    const [settings, setSettings] = useState<SetupSettings | null>(null)
    const [busy, setBusy] = useState(true)
    const [error, setError] = useState('')
    const [conflict, setConflict] = useState(false)
    const [notice, setNotice] = useState('')
    const active = useRef<AbortController | null>(null)
    const heading = useRef<HTMLHeadingElement>(null)

    function begin() {
        active.current?.abort()
        const controller = new AbortController()
        active.current = controller
        setBusy(true); setError(''); setNotice('')
        return controller
    }
    function accept(result: SetupDraft) {
        setDraft(result); setSettings(result.settings); setConflict(false)
    }
    async function load() {
        const controller = begin()
        try {
            const result = await api<SetupDraft>('/admin/setup/draft', { cache: 'no-store', signal: controller.signal })
            if (active.current === controller && !controller.signal.aborted) accept(result)
        } catch (e) {
            if (!controller.signal.aborted && active.current === controller) setError(e instanceof Error ? e.message : 'Could not load setup draft.')
        } finally {
            if (!controller.signal.aborted && active.current === controller) setBusy(false)
        }
    }
    useEffect(() => { void load(); return () => active.current?.abort() }, [session.user.id, session.csrfToken])

    async function save(step: SetupStep, targets: SetupTarget[] = []) {
        if (!draft || !settings || busy) return
        const controller = begin()
        try {
            let result = await api<SetupDraft>('/admin/setup/draft', {
                method: 'PUT', signal: controller.signal,
                body: JSON.stringify({ expectedRevision: draft.revision, currentStep: step, settings }),
            }, session.csrfToken)
            if (controller.signal.aborted || active.current !== controller) return
            accept(result); setNotice('Draft saved. Runtime configuration is unchanged.')
            for (const target of targets) {
                result = await api<SetupDraft>('/admin/setup/validate', {
                    method: 'POST', signal: controller.signal,
                    body: JSON.stringify({ expectedRevision: result.revision, target }),
                }, session.csrfToken)
                if (controller.signal.aborted || active.current !== controller) return
                accept(result)
            }
            if (step !== draft.currentStep) requestAnimationFrame(() => heading.current?.focus())
        } catch (e) {
            if (!controller.signal.aborted && active.current === controller) {
                setConflict(e instanceof ApiError && e.code === 'setup_revision_conflict')
                setError(e instanceof ApiError ? e.message : 'Could not save draft or complete checks. Your fields are still here; try again.')
            }
        } finally {
            if (!controller.signal.aborted && active.current === controller) setBusy(false)
        }
    }
    async function finish() {
        if (!draft || busy) return
        const controller = begin()
        try {
            const result = await api<SetupDraft>('/admin/setup/finish', {
                method: 'POST', signal: controller.signal, body: JSON.stringify({ expectedRevision: draft.revision }),
            }, session.csrfToken)
            if (controller.signal.aborted || active.current !== controller) return
            accept(result); onFinished?.()
            requestAnimationFrame(() => heading.current?.focus())
        } catch (e) {
            if (!controller.signal.aborted && active.current === controller) {
                setConflict(e instanceof ApiError && e.code === 'setup_revision_conflict')
                setError(e instanceof ApiError ? e.message : 'Could not finish setup. Your draft is unchanged; try again.')
                void api<SetupDraft>('/admin/setup/draft', { cache: 'no-store' }).then(result => { if (active.current === controller) setDraft(result) }, () => undefined)
            }
        } finally {
            if (!controller.signal.aborted && active.current === controller) setBusy(false)
        }
    }
    function submit(event: FormEvent<HTMLFormElement>) {
        event.preventDefault()
        const intent = (event.nativeEvent as SubmitEvent).submitter?.getAttribute('value')
        const index = steps.findIndex(step => step.key === draft?.currentStep)
        const next = intent === 'next' ? steps[index + 1]?.key : intent === 'back' ? steps[Math.max(0, index - 1)].key : draft?.currentStep
        const targets: SetupTarget[] = []
        if (intent === 'check') {
            if (next === 'general' || next === 'authentication') targets.push(next)
            if (next === 'providers' && settings) {
                if (settings.providers.tmdbEnabled) targets.push('tmdb')
                if (settings.providers.musicBrainzEnabled) targets.push('musicbrainz')
                if (settings.providers.igdbEnabled) targets.push('igdb')
            }
        }
        if (intent === 'check') {
            if (next === 'notifications' && settings?.notifications.appriseEnabled) targets.push('notifications')
        }
        if (intent === 'check-all' && draft) targets.push(...draft.blockers?.map(item => item.target) ?? [])
        if (intent === 'check-library') {
            const target = (event.nativeEvent as SubmitEvent).submitter?.getAttribute('data-target')
            if (target?.startsWith('library:')) targets.push(target as SetupTarget)
        }
        if (intent === 'check-all') { void save('review', targets); return }
        if (next) void save(next, targets)
    }
    function general(key: keyof SetupSettings['general'], value: string) {
        setSettings(current => current && ({ ...current, general: { ...current.general, [key]: value } }))
    }
    function authentication(key: 'issuer' | 'clientId', value: string) {
        setSettings(current => current && ({ ...current, authentication: { ...current.authentication, [key]: value } }))
    }
    function provider<K extends keyof SetupSettings['providers']>(key: K, value: SetupSettings['providers'][K]) {
        setSettings(current => current && ({ ...current, providers: { ...current.providers, [key]: value } }))
    }
    function source(kind: keyof SetupSettings['libraries'], patch: Partial<SetupSource>) {
        setSettings(current => current && ({ ...current, libraries: { ...current.libraries, [kind]: { ...current.libraries[kind], ...patch } } }))
    }
    function notifications<K extends keyof SetupSettings['notifications']>(key: K, value: SetupSettings['notifications'][K]) {
        setSettings(current => current && ({ ...current, notifications: { ...current.notifications, [key]: value } }))
    }
    function autoApprove(type: SetupSource['mediaTypes'][number], on: boolean) {
        setSettings(current => current && ({ ...current, policy: { autoApproveMediaTypes: on ? [...current.policy.autoApproveMediaTypes, type] : current.policy.autoApproveMediaTypes.filter(value => value !== type) } }))
    }
    const step = draft?.currentStep
    const completed = !!draft?.completedAt
    const editable = !completed && (step === 'general' || step === 'authentication' || step === 'providers' || step === 'libraries' || step === 'notifications' || step === 'policy')
    const blockers = draft?.blockers ?? []
    return <main className="shell draftWizard"><section className="card">
        <p className="eyebrow">Administrator · Shared setup draft</p>
        <h1>Setup · Inactive draft</h1>
        <p>Saving a draft does not activate or apply configuration. Server environment changes require an operator restart. Setup completes only when you finish the Review step with every required check passing.</p>
        {busy && !draft && <p role="status">Loading saved draft…</p>}
        {busy && draft && <p role="status">Updating shared draft… Please wait for saving and any requested checks.</p>}
        {error && <div role="alert" className="errorBox"><p>{error}</p>{!draft && <button className="button secondary" disabled={busy} onClick={() => void load()}>Retry loading draft</button>}</div>}
        {conflict && <div className="setupNotice"><p>The shared draft changed. Your unsaved fields have been kept. Reloading replaces these fields with the saved draft; copy any edits you want to keep before continuing.</p><button className="button secondary" disabled={busy} onClick={() => void load()}>Reload saved draft</button></div>}
        {notice && !busy && <p role="status">{notice}</p>}
        {draft && settings && <>
            <p>Revision {draft.revision} · Saved <time dateTime={draft.updatedAt}>{draft.updatedAt}</time></p>
            <ol className="draftSteps" aria-label="Setup steps">{steps.map(item => <li key={item.key} aria-current={step === item.key ? 'step' : undefined}>{item.label}</li>)}</ol>
            <h2 ref={heading} tabIndex={-1}>{completed ? 'Setup complete' : steps.find(item => item.key === step)?.label || 'Pending handoff'}</h2>
            {completed && <div role="status" className="setupNotice"><p>An administrator finished setup on <time dateTime={draft.completedAt!}>{draft.completedAt}</time>. The wizard no longer accepts changes. Sign-in remains OIDC-only. Runtime configuration still comes from the server environment.</p></div>}
            {!completed && <form className="setupForm" onSubmit={submit} aria-busy={busy}>
                <fieldset disabled={busy} className="draftFields">
                    <legend className="sr-only">{steps.find(item => item.key === step)?.label} settings</legend>
                    {step === 'general' && <>
                        <label>Instance name<input maxLength={120} value={settings.general.instanceName} onChange={e => general('instanceName', e.target.value)} /></label>
                        <label>Timezone<input maxLength={128} placeholder="America/New_York" value={settings.general.timezone} onChange={e => general('timezone', e.target.value)} /></label>
                        <label>Public URL<input type="url" maxLength={2048} value={settings.general.publicUrl} onChange={e => general('publicUrl', e.target.value)} /></label>
                    </>}
                    {step === 'authentication' && <>
                        <p>OIDC remains required. Discovery checks do not prove interactive login works. Existing sign-in and administrator bootstrap are unchanged.</p>
                        <label>OIDC issuer<input type="url" maxLength={2048} value={settings.authentication.issuer} onChange={e => authentication('issuer', e.target.value)} /></label>
                        <label>OIDC client ID<input maxLength={256} value={settings.authentication.clientId} onChange={e => authentication('clientId', e.target.value)} /></label>
                        <p>Client secret environment reference (read-only): <code>REQUESTARR_OIDC_CLIENT_SECRET</code>. Configure its value on the server, never here.</p>
                    </>}
                    {step === 'providers' && <>
                        <div className="draftProvider"><h3>TMDb</h3>
                            <label className="draftToggle"><input type="checkbox" checked={settings.providers.tmdbEnabled} onChange={e => provider('tmdbEnabled', e.target.checked)} />Enable TMDb</label>
                            <SecretField name="tmdb_read_token" label="TMDb read token" csrfToken={session.csrfToken} />
                            <p>Environment reference: <code>REQUESTARR_TMDB_READ_TOKEN</code></p>
                        </div>
                        <div className="draftProvider"><h3>MusicBrainz</h3>
                            <label className="draftToggle"><input type="checkbox" checked={settings.providers.musicBrainzEnabled} onChange={e => provider('musicBrainzEnabled', e.target.checked)} />Enable MusicBrainz</label>
                            <label>MusicBrainz contact email<input type="email" maxLength={254} value={settings.providers.musicBrainzContact} onChange={e => provider('musicBrainzContact', e.target.value)} aria-describedby="musicbrainzHint" /></label>
                            <p id="musicbrainzHint">A contact email is required when checking MusicBrainz. You can save an incomplete draft without one.</p>
                        </div>
                        <div className="draftProvider"><h3>IGDB</h3>
                            <label className="draftToggle"><input type="checkbox" checked={settings.providers.igdbEnabled} onChange={e => provider('igdbEnabled', e.target.checked)} />Enable IGDB</label>
                            <label>IGDB client ID<input maxLength={256} value={settings.providers.igdbClientId} onChange={e => provider('igdbClientId', e.target.value)} /></label>
                            <SecretField name="igdb_client_secret" label="IGDB client secret" csrfToken={session.csrfToken} />
                            <p>Environment reference: <code>REQUESTARR_IGDB_CLIENT_SECRET</code></p>
                        </div>
                    </>}
                    {step === 'libraries' && <>
                        <p>Choose which media types each Source is authoritative for. Draft settings remain inactive; checks do not sync inventory or change availability.</p>
                        {(['jellyfin', 'plex', 'romm', 'navidrome'] as const).map(kind => {
                            const media = kind === 'jellyfin' ? ['movie', 'tv'] : kind === 'plex' ? ['movie', 'tv', 'album', 'song'] : kind === 'romm' ? ['game'] : ['album', 'song']
                            const item = settings.libraries[kind]
                            const title = kind === 'romm' ? 'RomM' : kind[0].toUpperCase() + kind.slice(1)
                            return <div className="draftProvider" key={kind}>
                                <h3>{title}</h3>
                                <label className="draftToggle"><input type="checkbox" checked={item.enabled} onChange={e => source(kind, { enabled: e.target.checked })} />Enable {title}</label>
                                <label>{title} URL<input type="url" maxLength={2048} value={item.url} onChange={e => source(kind, { url: e.target.value })} /></label>
                                <fieldset><legend>Authoritative media types</legend>{media.map(type => <label className="draftToggle" key={type}><input type="checkbox" checked={item.mediaTypes.includes(type as SetupSource['mediaTypes'][number])} onChange={e => source(kind, { mediaTypes: e.target.checked ? [...item.mediaTypes, type as SetupSource['mediaTypes'][number]] : item.mediaTypes.filter(value => value !== type) })} />{type}</label>)}</fieldset>
                                <p>Server-side credential references only: <code>{item.usernameEnv || item.tokenEnv}</code>{item.passwordEnv ? <> and <code>{item.passwordEnv}</code></> : null}.</p>
                                <button className="button secondary" type="submit" value="check-library" data-target={`library:${kind}`} disabled={!item.enabled}>Check {title} connection</button>
                            </div>
                        })}
                    </>}
                    {step === 'notifications' && <>
                        <p>Global notifications go through an Apprise API. In-app notifications are always on.</p>
                        <div className="draftProvider"><h3>Apprise</h3>
                            <label className="draftToggle"><input type="checkbox" checked={settings.notifications.appriseEnabled} onChange={e => notifications('appriseEnabled', e.target.checked)} />Enable Apprise</label>
                            <label>Apprise API URL<input type="url" maxLength={2048} value={settings.notifications.appriseUrl} onChange={e => notifications('appriseUrl', e.target.value)} /></label>
                            <SecretField name="apprise_config_key" label="Apprise configuration key" csrfToken={session.csrfToken} />
                            <p>Configuration key environment reference: <code>{settings.notifications.appriseConfigKeyEnv}</code>. The URL must match <code>REQUESTARR_APPRISE_URL</code> on the server for the check to pass.</p>
                        </div>
                    </>}
                    {step === 'policy' && <>
                        <fieldset><legend>Skip administrator approval for</legend>
                            <p>Requests for media types left unchecked wait for an administrator to approve them.</p>
                            {mediaLabels.map(type => <label className="draftToggle" key={type}><input type="checkbox" checked={settings.policy.autoApproveMediaTypes.includes(type)} onChange={e => autoApprove(type, e.target.checked)} />{type}</label>)}
                        </fieldset>
                    </>}
                    {step === 'review' && <>
                        <p>Review the whole draft. Finish is available once every required check has passed for revision {draft.revision}.</p>
                        <section aria-label="Draft summary"><dl className="draftEnvironment">
                            <div><dt>General</dt><dd>{settings.general.instanceName} · {settings.general.timezone} · {settings.general.publicUrl}</dd></div>
                            <div><dt>Authentication</dt><dd>OIDC only · {settings.authentication.issuer} · client {settings.authentication.clientId}</dd></div>
                            <div><dt>Providers</dt><dd>TMDb {settings.providers.tmdbEnabled ? 'on' : 'off'} · MusicBrainz {settings.providers.musicBrainzEnabled ? 'on' : 'off'} · IGDB {settings.providers.igdbEnabled ? 'on' : 'off'}</dd></div>
                            {(['jellyfin', 'plex', 'romm', 'navidrome'] as const).map(kind => <div key={kind}><dt>{kind}</dt><dd>{settings.libraries[kind].enabled ? `${settings.libraries[kind].url} · ${settings.libraries[kind].mediaTypes.join(', ') || 'no media types'}` : 'Disabled'}</dd></div>)}
                            <div><dt>Notifications</dt><dd>In-app · Apprise {settings.notifications.appriseEnabled ? settings.notifications.appriseUrl : 'off'}</dd></div>
                            <div><dt>Request policy</dt><dd>Auto-approve: {settings.policy.autoApproveMediaTypes.join(', ') || 'none (administrator approval for all)'}</dd></div>
                        </dl></section>
                        {blockers.length > 0 ? <div role="alert" className="errorBox"><h3>Cannot finish yet</h3><ul>{blockers.map(item => <li key={item.target}>{item.target}: {blockerReason[item.reason]}</li>)}</ul></div> : <p role="status">All required checks have passed.</p>}
                    </>}
                    {!editable && !completed && step !== 'review' && <p>This step is unavailable. Return to General to continue.</p>}
                    <div className="draftActions">
                        {step !== 'general' && <button className="button secondary" type="submit" value="back">Save and back</button>}
                        {editable && <button className="button" type="submit" value="save">Save draft</button>}
                        {(step === 'general' || step === 'authentication') && <button className="button secondary" type="submit" value="check">Check connection</button>}
                        {step === 'notifications' && <button className="button secondary" type="submit" value="check" disabled={!settings.notifications.appriseEnabled}>Check Apprise connection</button>}
                        {step === 'review' && <button className="button secondary" type="submit" value="check-all" disabled={blockers.length === 0}>Run required checks</button>}
                        {step === 'review' && <button className="button" type="button" disabled={busy || blockers.length > 0} onClick={() => void finish()}>Finish setup</button>}
                        {step === 'providers' && <button className="button secondary" type="submit" value="check" disabled={!settings.providers.tmdbEnabled && !settings.providers.musicBrainzEnabled && !settings.providers.igdbEnabled}>Check enabled providers</button>}
                        {editable && <button className="button secondary" type="submit" value="next">Save and next</button>}
                        {!editable && step !== 'review' && <button className="button secondary" type="button" onClick={() => void save('general')}>Return to General</button>}
                    </div>
                </fieldset>
            </form>}
            <section aria-label="Connection check results">
                <p>Connection results apply only to saved revision {draft.revision}, not unsaved edits. Expired checks must be run again before relying on them.</p>
                {(draft.validations || []).filter(check => check.revision === draft.revision).map(check => <div key={check.target} role={check.valid ? 'status' : 'alert'} className={check.valid ? 'setupNotice' : 'errorBox'}>
                    <h3>{check.target} · {check.valid ? 'Check passed' : 'Check failed'}</h3>
                    <p>{check.code}: {check.message}</p>
                    <p>Checked <time dateTime={check.checkedAt}>{check.checkedAt}</time> · Expires <time dateTime={check.expiresAt}>{check.expiresAt}</time></p>
                </div>)}
            </section>
            <section aria-label="Server environment" className="setupNotice">
                <h2>Server environment</h2>
                <p>Secret values are never shown. Where a secret field is offered above, a saved value is stored encrypted on the server; other secrets are set only in the server environment. Apply changes and restart Requestarr separately; this draft remains inactive.</p>
                <dl className="draftEnvironment">{(draft.environment || []).map(item => <div key={item.name}>
                    <dt><code>{item.name}</code>{item.secret ? ' · Secret reference only' : ''}</dt>
                    <dd>Configured: {item.configured ? 'Yes' : 'No'} · Restart needed: {item.needsRestart ? 'Yes' : 'No'}
                        {!item.secret && item.desiredValue !== undefined && <p>Desired value: {item.desiredValue}</p>}
                    </dd>
                </div>)}</dl>
            </section>
        </>}
    </section></main>
}
