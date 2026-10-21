import { useEffect, useState } from 'react'
import { api, type Session } from '../api'

type InboxItem = { id: string; title: string; body: string; createdAt: string; readAt?: string }
export function NotificationsPage({ session }: { session: Session }) {
    const [items, setItems] = useState<InboxItem[]>([])
    const [error, setError] = useState('')
    useEffect(() => { void api<{ items: InboxItem[] }>('/notifications').then(result => setItems(result.items)).catch(() => setError('Could not load notifications')) }, [])
    async function markRead(id: string) {
        try { await api<void>(`/notifications/${id}/read`, { method: 'POST' }, session.csrfToken); setItems(current => current.map(item => item.id === id ? { ...item, readAt: new Date().toISOString() } : item)) }
        catch { setError('Could not update notification') }
    }
    return <main className="shell"><section className="card"><p className="eyebrow">Your account</p><h1>Notifications</h1>{error && <p role="alert" className="error">{error}</p>}{items.length === 0 ? <p>No notifications yet.</p> : <ul>{items.map(item => <li key={item.id}><strong>{item.title}</strong><p>{item.body}</p><time>{new Date(item.createdAt).toLocaleString()}</time>{!item.readAt && <button className="button secondary" onClick={() => void markRead(item.id)}>Mark read</button>}</li>)}</ul>}</section></main>
}
