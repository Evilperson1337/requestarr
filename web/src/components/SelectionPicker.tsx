import { useEffect, useState } from 'react'
import type { Selection } from '../api'

type Track = { id: string; position: number; title: string; lengthSeconds?: number }
type Season = { number: number; name?: string; episodeCount?: number; episodes?: { number: number; name?: string }[] }

const length = (seconds?: number) => (seconds ? `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, '0')}` : '')

// Whole item or a chosen part. onChange reports the Selection to send ({} for the whole item) and
// whether it can be sent (choosing a part needs at least one pick).
type Props<T> = { items: T[]; existing?: Selection; noun: string; onChange: (selection: Selection, valid: boolean) => void }

function Mode({ whole, setWhole, wholeLabel, partLabel, id }: { whole: boolean; setWhole: (whole: boolean) => void; wholeLabel: string; partLabel: string; id: string }) {
    return <div className="pickMode" role="radiogroup" aria-label="What to request">
        <label><input type="radio" name={id} checked={whole} onChange={() => setWhole(true)} /> {wholeLabel}</label>
        <label><input type="radio" name={id} checked={!whole} onChange={() => setWhole(false)} /> {partLabel}</label>
    </div>
}

export function TrackPicker({ items, existing, noun, onChange }: Props<Track>) {
    const have = new Set((existing?.tracks ?? []).map(t => t.id))
    const [whole, setWhole] = useState(true)
    const [picked, setPicked] = useState<Set<string>>(new Set())
    useEffect(() => {
        const tracks = items.filter(t => picked.has(t.id)).map(t => ({ id: t.id, position: t.position, title: t.title }))
        onChange(whole ? {} : { tracks }, whole || tracks.length > 0)
    }, [whole, picked, items])
    const open = items.filter(t => !have.has(t.id))
    function toggle(id: string) { setPicked(current => { const next = new Set(current); if (!next.delete(id)) next.add(id); return next }) }
    return <fieldset className="picker">
        <legend>What to request</legend>
        <Mode id="track-mode" whole={whole} setWhole={setWhole} wholeLabel={`Whole ${noun}`} partLabel="Choose tracks" />
        {!whole && <>
            <div className="pickActions">
                <span role="status">{picked.size} of {open.length} selected</span>
                <button type="button" className="linkButton" onClick={() => setPicked(new Set(open.map(t => t.id)))}>Select all</button>
                <button type="button" className="linkButton" onClick={() => setPicked(new Set())}>Clear</button>
            </div>
            <ul className="pickList" aria-label="Tracks">{items.map(t => <li key={t.id}>
                <label className={have.has(t.id) ? 'taken' : undefined}>
                    <input type="checkbox" checked={have.has(t.id) || picked.has(t.id)} disabled={have.has(t.id)} onChange={() => toggle(t.id)} />
                    <span className="pickIndex">{t.position}</span><span className="pickName">{t.title}</span>
                    <span className="pickMeta">{have.has(t.id) ? 'Already requested' : length(t.lengthSeconds)}</span>
                </label>
            </li>)}</ul>
        </>}
    </fieldset>
}

const key = (season: number, episode: number) => `${season}:${episode}`

export function SeasonPicker({ items, existing, noun, onChange }: Props<Season>) {
    const haveSeason = new Set(existing?.seasons ?? [])
    const haveEpisode = new Set((existing?.episodes ?? []).map(e => key(e.season, e.episode)))
    const [whole, setWhole] = useState(true)
    const [seasons, setSeasons] = useState<Set<number>>(new Set())
    const [episodes, setEpisodes] = useState<Set<string>>(new Set())
    useEffect(() => {
        const out: Selection = {}
        if (seasons.size) out.seasons = [...seasons].sort((a, b) => a - b)
        if (episodes.size) out.episodes = [...episodes].map(k => { const [season, episode] = k.split(':').map(Number); return { season, episode } }).sort((a, b) => a.season - b.season || a.episode - b.episode)
        onChange(whole ? {} : out, whole || seasons.size + episodes.size > 0)
    }, [whole, seasons, episodes, items])
    const covered = (season: number, episode: number) => haveSeason.has(season) || haveEpisode.has(key(season, episode))
    function toggleSeason(season: Season) {
        const on = seasons.has(season.number)
        setSeasons(current => { const next = new Set(current); if (on) next.delete(season.number); else next.add(season.number); return next })
        setEpisodes(current => new Set([...current].filter(k => !k.startsWith(`${season.number}:`))))
    }
    function toggleEpisode(season: Season, episode: number) {
        const all = (season.episodes ?? []).map(e => e.number).filter(n => !covered(season.number, n))
        const selected = (n: number) => seasons.has(season.number) || episodes.has(key(season.number, n))
        const nowOn = !selected(episode)
        const next = new Set(all.filter(n => (n === episode ? nowOn : selected(n))))
        if (next.size === all.length && all.length > 0) {
            setSeasons(current => new Set(current).add(season.number))
            setEpisodes(current => new Set([...current].filter(k => !k.startsWith(`${season.number}:`))))
        } else {
            setSeasons(current => { const copy = new Set(current); copy.delete(season.number); return copy })
            setEpisodes(current => new Set([...[...current].filter(k => !k.startsWith(`${season.number}:`)), ...[...next].map(n => key(season.number, n))]))
        }
    }
    const count = seasons.size + episodes.size
    return <fieldset className="picker">
        <legend>What to request</legend>
        <Mode id="season-mode" whole={whole} setWhole={setWhole} wholeLabel={`Whole ${noun}`} partLabel="Choose seasons or episodes" />
        {!whole && <>
            <div className="pickActions">
                <span role="status">{count === 0 ? 'Nothing selected' : `${seasons.size} ${seasons.size === 1 ? 'season' : 'seasons'}, ${episodes.size} ${episodes.size === 1 ? 'episode' : 'episodes'}`}</span>
                <button type="button" className="linkButton" onClick={() => { setSeasons(new Set(items.filter(s => !haveSeason.has(s.number)).map(s => s.number))); setEpisodes(new Set()) }}>Select all</button>
                <button type="button" className="linkButton" onClick={() => { setSeasons(new Set()); setEpisodes(new Set()) }}>Clear</button>
            </div>
            <ul className="pickList" aria-label="Seasons">{items.map(season => {
                const partial = !seasons.has(season.number) && [...episodes].some(k => k.startsWith(`${season.number}:`))
                const label = season.number === 0 ? 'Specials' : `Season ${season.number}`
                return <li key={season.number}>
                    <label className={haveSeason.has(season.number) ? 'taken' : undefined}>
                        <input type="checkbox" aria-label={label} checked={haveSeason.has(season.number) || seasons.has(season.number)} disabled={haveSeason.has(season.number)} ref={el => { if (el) el.indeterminate = partial }} onChange={() => toggleSeason(season)} />
                        <span className="pickName">{season.name || label}</span>
                        <span className="pickMeta">{haveSeason.has(season.number) ? 'Already requested' : season.episodeCount ? `${season.episodeCount} ${season.episodeCount === 1 ? 'episode' : 'episodes'}` : ''}</span>
                    </label>
                    {!!season.episodes?.length && <details>
                        <summary>Episodes</summary>
                        <ul className="pickList nested" aria-label={`${label} episodes`}>{season.episodes.map(e => <li key={e.number}>
                            <label className={covered(season.number, e.number) ? 'taken' : undefined}>
                                <input type="checkbox" checked={covered(season.number, e.number) || seasons.has(season.number) || episodes.has(key(season.number, e.number))} disabled={covered(season.number, e.number)} onChange={() => toggleEpisode(season, e.number)} />
                                <span className="pickIndex">{e.number}</span><span className="pickName">{e.name || `Episode ${e.number}`}</span>
                            </label>
                        </li>)}</ul>
                    </details>}
                </li>
            })}</ul>
        </>}
    </fieldset>
}

