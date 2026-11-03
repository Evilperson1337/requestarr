import { useEffect, useRef, type KeyboardEvent, type ReactNode } from 'react'

// A dialog that traps focus, closes on Escape or a click outside, locks the page
// behind it and hands focus back to whatever opened it.
export function Modal({ labelledBy, onClose, className = '', children }: { labelledBy: string; onClose: () => void; className?: string; children: ReactNode }) {
    const ref = useRef<HTMLElement>(null)
    useEffect(() => {
        const opener = document.activeElement as HTMLElement | null
        const overflow = document.body.style.overflow
        document.body.style.overflow = 'hidden'
        ref.current?.focus()
        return () => { document.body.style.overflow = overflow; opener?.focus() }
    }, [])
    function keyboard(event: KeyboardEvent) {
        if (event.key === 'Escape') { event.stopPropagation(); onClose(); return }
        if (event.key !== 'Tab' || !ref.current) return
        const controls = [...ref.current.querySelectorAll<HTMLElement>('button:not(:disabled), input:not(:disabled), textarea:not(:disabled), select:not(:disabled), a[href]')]
        const first = controls[0], last = controls[controls.length - 1]
        if (event.shiftKey && (document.activeElement === first || document.activeElement === ref.current)) { event.preventDefault(); last?.focus() }
        else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus() }
    }
    return <div className="overlay" onMouseDown={event => { if (event.target === event.currentTarget) onClose() }}>
        <section ref={ref} tabIndex={-1} role="dialog" aria-modal="true" aria-labelledby={labelledBy} className={`libModal ${className}`} onKeyDown={keyboard}>
            <button className="close" type="button" aria-label="Close" onClick={onClose}>×</button>
            {children}
        </section>
    </div>
}
