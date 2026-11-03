import { useId, useState, type ReactNode } from 'react'
import { ProviderLogo } from './ProviderLogo'

export type Tone = 'on' | 'off' | 'warn'

// One service Requestarr talks to: its mark and name, whether it is working, what
// it last did, and (folded away until wanted) how it is configured.
export function IntegrationCard({ slug, name, status, tone, meta, actions, defaultOpen = false, configurable = true, setUp = false, children }: {
    slug: string; name: string; status: string; tone: Tone; meta?: ReactNode; actions?: ReactNode; defaultOpen?: boolean; configurable?: boolean; setUp?: boolean; children?: ReactNode
}) {
    const id = useId()
    const [open, setOpen] = useState(defaultOpen)
    return <li className="integration" aria-labelledby={`${id}-name`}>
        <div className="integrationHead">
            <ProviderLogo slug={slug} />
            <div className="integrationTitle">
                <h3 id={`${id}-name`}>{name}</h3>
                <span className={`integrationState ${tone}`}>{status}</span>
            </div>
            <div className="integrationActions">
                {actions}
                {configurable && <button type="button" className="button secondary" aria-expanded={open} aria-controls={`${id}-body`} onClick={() => setOpen(!open)}>{open ? 'Hide settings' : setUp ? 'Set up' : 'Settings'}</button>}
            </div>
        </div>
        {meta && <p className="integrationMeta">{meta}</p>}
        {configurable && <div id={`${id}-body`} className="integrationBody" hidden={!open}>{children}</div>}
    </li>
}
