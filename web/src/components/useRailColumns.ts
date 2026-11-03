import { useEffect, useRef, useState } from 'react'

const minCard = 168
const gap = 18

// How many covers fit on one row of a rail, kept in step with the CSS grid's
// minimum card width, so a rail shows whole rows and never an empty slot.
export function useRailColumns<T extends HTMLElement>() {
    const ref = useRef<T>(null)
    const [columns, setColumns] = useState(6)
    useEffect(() => {
        const el = ref.current
        if (!el) return
        const measure = () => setColumns(Math.max(2, Math.floor((el.clientWidth + gap) / (minCard + gap))))
        measure()
        const observer = new ResizeObserver(measure)
        observer.observe(el)
        return () => observer.disconnect()
    }, [])
    return { ref, columns }
}
