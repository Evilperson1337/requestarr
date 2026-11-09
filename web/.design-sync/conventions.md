# Requestarr UI conventions

Requestarr is a dark-only app: near-black ground, white text, one lime accent that means "available / working". The components are plain React with a single global stylesheet (`styles.css`, which imports `_ds_bundle.css`). There is no provider and no CSS-in-JS: **styling is global CSS class names plus `var(--*)` tokens.**

## Setup
- Always load `styles.css`. It sets `body { background: var(--ground); color: var(--text); margin: 0 }`. Without that ground, white-on-dark text and the BrandMark brackets are invisible. Do not set a light background.
- No wrapper component is needed. Components import from the bundle global `RequestarrUI`.

## Tokens (read `styles.css` / `_ds_bundle.css` `:root` for the full list)
- Surfaces: `--ground` (page), `--surface` (cards, dialogs), `--raised` (controls, placeholders), `--border`, `--rule`, `--control`.
- Text: `--text`, `--muted`, `--faint`.
- Meaning: `--accent` / `--positive` (lime), `--negative` (red), `--warning` (amber).
- Pills: `--pill-{positive,negative,warning,neutral}-{bg,ink}`.
- Shape: `--radius-card` (20px), `--gap` (14px). Buttons are pill-shaped (radius 999px).
- Font: system-ui sans stack. No webfonts to load.

## Class vocabulary used by the components
- Buttons: `button` (white pill, primary) and `button secondary` (dark pill). Use on `<button type="button">`.
- Layout glue the components rely on: `setting` / `settingLabel` / `settingPanel` (SettingsSection), `integration` / `integrationState on|warn` (IntegrationCard), `coverRail` / `railCard` / `railCover` / `railTitle` / `railKind` / `railStatus status-<requested|approved|available|denied|cancelled>` (rails), `overlay` / `libModal` (Modal), `field` (form fields), `badge status-<state>` (status badges).
- Prefer the components. For your own glue use inline styles with the tokens above (e.g. `style={{ background: 'var(--surface)', borderRadius: 'var(--radius-card)', padding: 20 }}`); don't invent new class names, they will not resolve.

## Components
`BrandMark`, `ProviderLogo` (slugs: jellyfin, plex, navidrome, audiobookshelf, romm, tmdb, musicbrainz, igdb, apprise), `SettingsSection`, `IntegrationCard` (renders an `<li>`, put it in a `<ul>`), `Modal` (fixed overlay; needs `labelledBy` = id of its heading), `RequestCover`, `RequestRail`, `LibraryRail`. Each has `<Name>.d.ts` and `<Name>.prompt.md` under `components/general/<Name>/`; read them for props. Media types: movie, tv, game, album, song, audiobook. Artwork is 2:3 poster, or 1:1 square for albums/audiobooks (`ArtworkKind: 'square'`).

## Example
```tsx
<ul style={{ listStyle: 'none', margin: 0, padding: 0, display: 'grid', gap: 12, maxWidth: 640 }}>
  <IntegrationCard slug="jellyfin" name="Jellyfin" status="Connected" tone="on"
    meta="Last synced 4 minutes ago: 1,204 items">
    <p>Server URL and API key are set.</p>
  </IntegrationCard>
</ul>
```
