# SignalLuxe redesign: gap assessment and plan

Status: needs-triage
Branch: `redesign` (off `main` at d5b15cc)
Source: `references/requestarr-signalluxe-handoff/` (untracked on `main`; read its README, `agent/IMPLEMENTATION_BRIEF.md`, `specs/`)

## What the handoff asks for

A restyle, not a feature project: a dark-only navy/magenta/violet theme with a permanent sidebar, an
atmospheric SVG backdrop, media cards and rails, status badges, integration cards and a seven-item
primary nav (Request, Library, Requests, Issues, Notifications, Settings, **Logs**). The mockups are
visual direction only. The handoff itself lists what to ignore: Radarr/Sonarr/download content, "Trending"
labels, fabricated counts, "connected" states that are not real, search/approve buttons the server cannot honour.

## Where we are now

- Look: pure black ground, one lime accent (`#c6f24e`), flat panels, no gradients, no shadows, a top-left
  "rail" of text links. Tokens live in `:root` at the top of `web/src/styles.css` (about 20 variables).
- One 2,617-line global stylesheet, plain CSS, camelCase class names, no CSS modules, no UI library.
  `web/scripts/check-classnames.mjs` fails the check if a class string has no rule, so renames must move together.
- Shell is inline in `web/src/App.tsx` (`appShell` > `rail` > `nav`, `appMain`). Page routing is a
  hand-rolled `path` switch; no router library.
- Already matching the handoff: poster 2:3 and square album art (D-decisions), rails with page arrows, the
  Settings sub-tabs (Integrations, Authentication, Activity, Matches, Users, Logs), real count badges
  (awaiting approval, open issues, unread), touch-target and a11y Playwright suites.

## Conflicts to settle before coding

1. **Logs as a primary nav item.** Logs exists today only as a Settings tab (`ServerLogs`, `DiagnosticsTokens`),
   gated on `capabilities.manageAccess` (superuser). The handoff wants one canonical Logs route. Plan: add a
   `/logs` route and nav entry shown only to `manageAccess`; make the Settings "Logs" tab a link to it (no duplicate page).
   The log endpoint and its redaction already exist, so no backend work. Diagnostics tokens stay with Settings.
2. **`AGENTS.md` says the UI follows `references/mockups/final/` and `docs/UI_UX.md`.** The handoff replaces
   that visual direction. Per the repo rules this needs a new entry in `docs/DECISIONS.md` (next D-number)
   that supersedes the visual part of `UI_UX.md`, and an `AGENTS.md` line update. Do not diverge silently.
3. **Dark-only.** Current app is also dark, so no light theme is lost.
4. **Handoff vs the design-taste skill's house rules.** The skill bans gradients, glows, thin borders and glass;
   the handoff specifies gradient CTAs, glowing active nav and bordered panels. The handoff is the owner's
   explicit brand, so it wins; treat it as an existing design system (`--keep` palette and type) and pass
   `--allow` for those rules if the inspector is run. Keep the handoff's own limits: glow only on active nav
   and the primary action, no glow on nested panels, no continuous animation.
5. **Fonts.** The handoff names Inter but forbids bundling binaries. Use the system fallback stack
   (`Inter, ui-sans-serif, system-ui, sans-serif`); no `@font-face`.
6. **Mockup text that must not ship:** "Trending Requests" (use the real section names: "Your recent
   requests", "New movies and shows in your libraries", and so on), the "Request" button label (the real
   action is Preview and its search semantics, including album search on Enter, must not change), the
   New Request button, fabricated stats, Radarr/Sonarr log lines, a global search box.
7. **Existing behavior to preserve:** TMDB attribution (API terms, `tmdb-attribution.spec.ts`), the Get started
   link, the forced password-change and sign-in screens, role-gated nav items, bookmarkable Requests filters.

## Work breakdown (the handoff's slices, mapped to this repo)

| Slice | Work | Files | Risk |
| --- | --- | --- | --- |
| A. Foundation and shell | Replace `:root` tokens with the SignalLuxe set (keep existing variable names as aliases first so the 2.6k lines keep working, then migrate). Add backdrop (`web/src/assets/` copy of the SVG, dark overlay). Extract `AppShell`, `SidebarNav` and `UserMenu` from `App.tsx`; add nav icons (inline SVG, labelled), the Logs entry, the sidebar width 264px, and a drawer for narrow screens with a visible toggle. Mark swapped for the new emblem only if approved (handoff says it is not a production logo). | `styles.css` (1-160, 546-620, 1127-1170), `App.tsx`, `BrandMark.tsx`, new `components/AppShell.tsx` | Medium: touch-target and a11y specs depend on the rail |
| B. Media cards, rails, Request home | Restyle the existing rail, cover and card classes; centred title and search bar; category chips as the existing radio group restyled (keep radios, they are the semantics). | `HomePage`, `RequestRail`, `RequestCover`, `Artwork`, `styles.css` 1396-1540 | Low: skins only |
| C. Library and Requests | Tabs and filters as chips, grid density, status badges (text plus colour, never colour alone), thumbnail rows. | `LibraryPage`, `RequestsPage`, `LibraryTags` | Low |
| D. Issues and Notifications | Row and card styling; unread distinguishable without colour. | `IssuesPage`, `NotificationsPage` | Low |
| E. Settings | Integration cards grouped Media libraries / Metadata / Notifications with real state; forms keep their validation; no secret values. | `SettingsPage`, `IntegrationCard`, `ProviderIntegrations`, `SettingsSection` | Medium: largest surface |
| F. Logs | New route as in conflict 1; restyle `ServerLogs` as an accessible table. | `App.tsx`, `ServerLogs`, `SettingsPage` | Low |
| G. Auth screens, modals, polish | Sign-in, local sign-in, change password, Get started, all modals; contrast on the backdrop; reduced motion; screenshots at 360, 768, 1440; docs. | `SignInPage`, `Modal`, others | Medium |

Every slice ends with `scripts/check.sh` (fmt, test, vet, integration, web typecheck, Playwright).
Slices B to F are CSS-and-markup only; the only new behavior is the `/logs` route and nav entry.
Add tests for: Logs nav visibility by capability, the `/logs` route guard, and the Settings Logs tab link.

## Known costs and risks

- The stylesheet is one file with order-dependent rules; a token swap with aliases is the safe first step.
  Splitting it into per-area files is optional and should be its own change.
- Contrast: the pink on navy gradient button, muted text `#868BA9` on the surface, and text over the backdrop
  need measured WCAG AA checks (the handoff says tokens were eyeballed, not sampled). `a11y.spec.ts` (axe) will catch some.
- `web/ds-bundle` and `web/design-sync` mirror the presentational components to Claude Design (last
  commit). Re-sync after Slice A if that mirror is still wanted.
- Playwright tests that assert colours or class names (about 7 matches) will need updates.
- Performance: a fixed full-page SVG backdrop and per-card shadows on long Library grids; use
  `background-attachment: scroll` on small screens (the starter CSS already does) and no shadow on cards.

## Open questions for the owner

1. Approve superseding the existing mockups and `UI_UX.md` (conflict 2)?
2. Is the starter emblem acceptable as the brand mark, or keep the current one?
3. Should Logs show for superusers only, as today?
4. Land as one branch or merge slice by slice?

## Status (2026-11-09)

Done on `redesign`: tokens and aliases, backdrop, sidebar with drawer below 900px, Logs route and nav (admin and superuser, D044),
starter emblem, bordered navy panels, unread marker, status colours, test updates. Checked by screenshot: Request, Requests,
Library, Settings, Logs, Notifications, sign-in, phone menu. Not done: Issues and modals were not screenshotted; no per-category
carousel panels (rails are not wrapped in panels, to avoid nested panels); the drawer breakpoint is one cutoff (900px).
