import { useEffect, useState } from 'react'
import { api, type ApprovalSettings } from '../api'
import { SettingsSection } from './SettingsSection'

const mediaTypes = [['movie', 'Movie'], ['tv', 'TV'], ['album', 'Album'], ['song', 'Song'], ['game', 'Game'], ['audiobook', 'Audiobook']] as const

// The approval policy (D036): media types whose new requests start approved.
export function ApprovalPolicy({ csrfToken, onRequestError }: { csrfToken: string; onRequestError: (error: unknown) => void }) {
    const [chosen, setChosen] = useState<string[] | null>(null)
    const [message, setMessage] = useState('')
    const [error, setError] = useState('')
    useEffect(() => {
        api<ApprovalSettings>('/admin/settings/approvals', { cache: 'no-store' }).then(result => setChosen(result.autoApproveTypes)).catch(e => { onRequestError(e); setError('Could not load the approval settings') })
    }, [onRequestError])
    async function toggle(type: string, on: boolean) {
        if (!chosen) return
        const next = mediaTypes.map(([key]) => key as string).filter(key => key === type ? on : chosen.includes(key))
        setMessage(''); setError(''); setChosen(next)
        try {
            const saved = await api<ApprovalSettings>('/admin/settings/approvals', { method: 'PUT', body: JSON.stringify({ autoApproveTypes: next }) }, csrfToken)
            setChosen(saved.autoApproveTypes)
            const names = mediaTypes.filter(([key]) => saved.autoApproveTypes.includes(key)).map(([, label]) => label)
            setMessage(names.length ? `Requests for ${names.join(', ')} are approved automatically.` : 'Every request waits for an administrator to approve it.')
        } catch (e) { setChosen(chosen); onRequestError(e); setError(e instanceof Error ? e.message : 'Could not save the approval settings') }
    }
    return <SettingsSection title="Approvals" description="Requests of the chosen types start approved, with a note in their history that a policy approved them. Everything else waits for an administrator. This applies to new requests only; requests already waiting stay as they are.">
        {error && <p role="alert" className="error">{error}</p>}
        {chosen && <fieldset><legend>Approve these automatically</legend>{mediaTypes.map(([key, label]) => <label key={key}><input type="checkbox" checked={chosen.includes(key)} onChange={event => void toggle(key, event.target.checked)} /> {label}</label>)}</fieldset>}
        {message && <p role="status">{message}</p>}
    </SettingsSection>
}
