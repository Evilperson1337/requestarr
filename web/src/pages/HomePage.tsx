export function HomePage() {
  return (
    <main className="shell">
      <section className="card hero">
        <p className="eyebrow">Requestarr · V1 scaffold</p>
        <h1>Request Something</h1>
        <p>Paste a TMDB, MusicBrainz, or IGDB link. Successful resolution will open the universal request modal.</p>
        <label className="urlBox"><span>🔗</span><input aria-label="Request URL" placeholder="Paste a supported link…" /></label>
        <div className="providers"><span>TMDB · Movies & TV</span><span>MusicBrainz · Songs & Albums</span><span>IGDB · Games</span></div>
      </section>
      <section className="card"><h2>Implementation scaffold</h2><p>The final UI decisions are documented in <code>docs/UI_UX.md</code> and <code>references/mockups/final/</code>.</p></section>
    </main>
  )
}
