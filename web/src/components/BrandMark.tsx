// The Requestarr mark: the SignalLuxe starter emblem (D044), a four-point spark in the accent pink.
// Decorative: the name always sits beside it.
export function BrandMark({ size = 24 }: { size?: number }) {
    return <svg className="brandMark" width={size} height={size} viewBox="0 0 64 64" aria-hidden="true" focusable="false" fill="none">
        <path d="M32 4L39 25L60 32L39 39L32 60L25 39L4 32L25 25L32 4Z" stroke="currentColor" strokeWidth="3" strokeLinejoin="round" />
        <path d="M32 13L37 27L51 32L37 37L32 51L27 37L13 32L27 27L32 13Z" fill="var(--ra-violet)" fillOpacity=".4" />
    </svg>
}
