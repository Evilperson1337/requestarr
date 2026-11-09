import type { ReactNode } from 'react'
import { BrandMark } from './BrandMark'

// The signed-out frame (D044): the SignalLuxe backdrop, the wordmark with its tagline, and one glass card.
// Icons are decorative; every chip and button carries its own text.
const icon = { width: 20, height: 20, viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', strokeWidth: 1.7, strokeLinecap: 'round', strokeLinejoin: 'round', 'aria-hidden': true, focusable: false } as const

const kinds: { name: string; glyph: ReactNode }[] = [
    { name: 'Movies', glyph: <><rect x="3" y="4" width="18" height="16" rx="2" /><path d="M7 4v16M17 4v16M3 9h4M3 15h4M17 9h4M17 15h4" /></> },
    { name: 'TV', glyph: <><rect x="3" y="7" width="18" height="13" rx="2" /><path d="M8 3l4 4 4-4" /></> },
    { name: 'Music', glyph: <><path d="M9 18V6l10-2v12" /><circle cx="6.5" cy="18" r="2.5" /><circle cx="16.5" cy="16" r="2.5" /></> },
    { name: 'Games', glyph: <><path d="M7 7h10a4 4 0 014 4l-1 5a2.5 2.5 0 01-4.4 1.2L14 15h-4l-1.6 2.2A2.5 2.5 0 014 16l-1-5a4 4 0 014-4z" /><path d="M8 10v3M6.5 11.5h3M15.5 10.5h.01M17.5 12.5h.01" /></> },
    { name: 'Audiobooks', glyph: <><path d="M12 6c-2-1.5-5-2-8-1.5v13c3-.5 6 0 8 1.5 2-1.5 5-2 8-1.5v-13C17 4 14 4.5 12 6zM12 6v13" /></> },
]

export function AuthScreen({ children, wide = false }: { children: ReactNode; wide?: boolean }) {
    return <div className="authScreen">
        <header className="authBar">
            <span className="authWordmark"><BrandMark size={34} /><span>Request<span>arr</span></span></span>
            <p>A shared media request &amp; availability portal</p>
        </header>
        <main className="authStage">
            <section className={wide ? 'authGlass wide' : 'authGlass'}>
                <span className="authEmblem" aria-hidden="true">
                    <svg {...icon} width={40} height={40} strokeWidth={1.5}><path d="M4 10h16v10H4zM4 10l2.5-5 3 1.5M9.5 10l2.5-5 3 1.5M15 10l2.5-5 3 1.5" /><path d="M8 15h5" /></svg>
                </span>
                {children}
            </section>
        </main>
    </div>
}

export function MediaKinds() {
    return <ul className="authKinds" aria-label="What you can request">
        {kinds.map(kind => <li key={kind.name}><svg {...icon}>{kind.glyph}</svg>{kind.name}</li>)}
    </ul>
}
