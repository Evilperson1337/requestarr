import { useCallback, useEffect, useState } from 'react'
import { api, type Checklist, type ChecklistItem, type Session } from '../api'
import { followLink, navigate } from '../navigation'

// Where each item is applied. The page never asks for a value itself (D038).
const targets: Record<ChecklistItem['key'], string> = {
    libraries: '/settings?tab=integrations#group-libraries',
    metadata: '/settings?tab=integrations#group-metadata',
    notifications: '/settings?tab=integrations#group-notifications',
    oidc: '/settings?tab=authentication',
    people: '/settings?tab=users',
}

export function GetStartedPage({ session, onChanged }: { session: Session; onChanged: (visible: boolean) => void }) {
    const [checklist, setChecklist] = useState<Checklist | null>(null)
    const [error, setError] = useState('')
    const [busy, setBusy] = useState(false)

    const load = useCallback(async (signal?: AbortSignal) => {
        setError('')
        try {
            const result = await api<Checklist>('/admin/checklist', { cache: 'no-store', signal })
            if (signal?.aborted) return
            setChecklist(result)
            onChanged(result.visible)
        } catch (e) {
            if (signal?.aborted) return
            setError(e instanceof Error ? e.message : 'Could not load the checklist')
        }
    }, [onChanged])
    useEffect(() => {
        const controller = new AbortController()
        void load(controller.signal)
        return () => controller.abort()
    }, [load, session.user.id])

    async function dismiss() {
        if (busy) return
        setBusy(true); setError('')
        try {
            await api<void>('/admin/checklist/dismiss', { method: 'POST' }, session.csrfToken)
            onChanged(false)
            navigate('/')
        } catch (e) {
            setError(e instanceof Error ? e.message : 'Could not dismiss the checklist')
        } finally { setBusy(false) }
    }

    const group = (required: boolean) => checklist?.items.filter(item => item.required === required) ?? []
    const section = (label: string, required: boolean, intro: string) => <section aria-labelledby={`checklist-${label}`}>
        <h2 id={`checklist-${label}`}>{label}</h2>
        <p>{intro}</p>
        <ul>{group(required).map(item => <li key={item.key}>
            <span className={`checkState ${item.done ? 'done' : ''}`}>{item.done ? 'Done' : 'To do'}</span>
            <h3 className="checkTitle">{item.title}</h3>
            <a className="button secondary" href={targets[item.key]} onClick={followLink} aria-label={`${item.done ? 'Review' : 'Open'} Settings for ${item.title}`}>{item.done ? 'Review' : 'Open Settings'}</a>
            {item.detail && <p className="checkDetail">{item.detail}</p>}
        </li>)}</ul>
    </section>
    return <main className="shell checklist">
        <h1>Get started</h1>
        <p>Requestarr is signed in and ready. These steps are worked out from how the instance is set up right now; each one opens the part of Settings that applies it.</p>
        {error && <div role="alert" className="errorBox"><p>{error}</p><button type="button" className="button secondary" onClick={() => void load()}>Try again</button></div>}
        {!checklist && !error && <p role="status">Loading the checklist…</p>}
        {checklist && <>
            {checklist.dismissed && <p role="status">This checklist was dismissed. It stays here for reference.</p>}
            {section('Required', true, 'Without these, people cannot search or see what you already have.')}
            {section('Optional', false, 'Worth a look, but Requestarr works without them.')}
            {!checklist.dismissed && <div className="checkActions"><button type="button" className="button secondary" disabled={busy} onClick={() => void dismiss()}>Dismiss checklist</button></div>}
        </>}
    </main>
}
