# design-sync notes (Requestarr UI)

- Requestarr's `web/` is an app, not a library. `web/design-sync/` is a throwaway library build: `src/index.ts` re-exports only components that render from props alone; `sh web/design-sync/build.sh` (run from repo root; it cds to web/) builds `design-sync/dist/` (vite lib, ES + one CSS file, svg/png inlined) and emits `.d.ts` into `dist/design-sync/src/`.
- Run the converter from `web/` with `--node-modules ./node_modules --entry ./design-sync/dist/index.js --out ./ds-bundle`. `.design-sync/` lives in `web/`, so run from there.
- Scope is the presentational set only. The other ~20 components call the API (fetch/session) and are intentionally excluded. To add one, export it from `design-sync/src/index.ts`, write a preview, re-sync.
- `Artwork.tsx` is a placeholder stub, deliberately not exported.
- The generated card html hard-codes `body{background:#fff}` (no config knob), but Requestarr is dark-only. Every authored preview wraps its cells in a `G` div using `var(--ground)`/`var(--text)`. Keep that wrapper in new previews or text and BrandMark brackets are invisible.
- Modal is `position: fixed`; its preview wraps it in a `transform: translateZ(0)` 720x560 box and uses `cardMode: single`, viewport 760x600, so the open dialog renders inside the card.
- Artwork in previews is inline SVG data URIs (no network).
- `SettingsSection`, `RequestRail`, `LibraryRail` use `cardMode: column`.

## Re-sync risks
- Everything depends on `src/styles.css` class names; a class rename silently unstyles the components and the `conventions.md` vocabulary. Re-run the conventions validation after CSS changes.
- Preview data (RequestItem / RecentLibraryItem shapes) is inlined in `previews/`; update if `src/api.ts` types change.
- `dist/` must be rebuilt (`build.sh`) before every re-sync, since `design-sync/dist` is gitignored.
- Verified with Playwright 1.64.1 (chromium-1248 cached). Fonts: system-ui stack only, nothing to ship.
