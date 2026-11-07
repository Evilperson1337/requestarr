import { useEffect, useId, useState } from 'react'
import { api, type AdminUser, type Session } from '../api'

// Offers the people a request can be made for. It is for roles that can manage
// requests only (the server's capability answer decides, and the server refuses
// the field from anyone else). An empty value means the signed-in person asks
// for themselves.
export function RequesterPicker({ session, value, onChange, disabled }: { session: Session; value: string; onChange: (userId: string) => void; disabled?: boolean }) {
    const id = useId()
    const allowed = session.capabilities.manageRequests
    const [people, setPeople] = useState<AdminUser[]>([])
    const [failed, setFailed] = useState(false)
    useEffect(() => {
        if (!allowed) return
        let current = true
        api<{ users: AdminUser[] }>('/admin/users', { cache: 'no-store' })
            .then(result => { if (current) setPeople(result.users.filter(person => person.canRequest && person.id !== session.user.id)) })
            .catch(() => { if (current) setFailed(true) })
        return () => { current = false }
    }, [allowed, session.user.id])
    if (!allowed) return null
    return <div className="field requesterPicker">
        <label htmlFor={id}>Request for</label>
        <select id={id} value={value} disabled={disabled} onChange={event => onChange(event.target.value)}>
            <option value="">Myself</option>
            {people.map(person => <option key={person.id} value={person.id}>{person.displayName || person.email || person.id}</option>)}
        </select>
        <p className="fieldHelp">{failed ? 'The list of people could not be loaded.' : 'Choose a person to make them the requester. They are notified of its progress.'}</p>
    </div>
}
