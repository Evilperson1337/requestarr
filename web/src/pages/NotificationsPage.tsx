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
    return <main className="shell"><section className="feed"><h1>Notifications</h1>{error && <p role="alert" className="error">{error}</p>}{items.length === 0 ? <p>No notifications yet.</p> : <ul className="feedList">{items.map(item => <li key={item.id} className={item.readAt ? 'read' : 'unread'}><div><strong>{item.title}</strong><p>{item.body}</p><time dateTime={item.createdAt}>{new Date(item.createdAt).toLocaleString()}</time></div>{!item.readAt && <button className="button secondary" onClick={() => void markRead(item.id)}>Mark read</button>}</li>)}</ul>}</section></main>
}
