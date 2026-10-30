import { useId, type ReactNode } from 'react'

// One settings concern: its name and what it is on the left, its controls on the right.
export function SettingsSection({ title, description, headingId, children }: { title: string; description?: ReactNode; headingId?: string; children: ReactNode }) {
    const generated = useId()
    const id = headingId ?? generated
    return <section className="setting" aria-labelledby={id}>
        <div className="settingLabel"><h2 id={id}>{title}</h2>{description && <p>{description}</p>}</div>
        <div className="settingPanel">{children}</div>
    </section>
}
