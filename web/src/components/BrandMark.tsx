// The Requestarr mark: an item held in a library's brackets. The brackets take the
// text colour and the item takes the accent, the one colour the app keeps for
// what is available. Decorative: the name always sits beside it.
export function BrandMark({ size = 22 }: { size?: number }) {
    return <svg className="brandMark" width={size} height={size} viewBox="0 0 24 24" aria-hidden="true" focusable="false">
        <path d="M8.5 3.5H4.5v17h4M15.5 3.5h4v17h-4" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="square" />
        <circle cx="12" cy="12" r="3.4" fill="var(--accent)" />
    </svg>
}
