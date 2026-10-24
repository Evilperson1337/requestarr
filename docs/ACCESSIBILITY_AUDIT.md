# Accessibility and responsive audit

Scope: Home, request preview dialog, Requests, Notifications, Settings and every
Setup wizard step, at 1280px and 360px wide. Accessibility and responsive layout
take precedence over pixel matching the mockups (AGENTS.md).

## How it is checked

`web/tests/a11y.spec.ts` runs in `make web-test` and CI against the mocked API:

- axe-core (`@axe-core/playwright`, a dev-only dependency) with the WCAG 2.0/2.1
  A and AA rule sets on each page above at both widths: 0 violations required.
  This covers the automated part of screen-reader support (roles, names, labels,
  contrast). axe best-practice rules (landmarks, heading order) are not enabled.
- No horizontal page scroll at 360px (`scrollWidth <= clientWidth`); the test
  names the overflowing elements on failure.
- A keyboard test for the request dialog.

Remaining gaps (why ticket 26 is not fully closed):

- No manual pass with a real screen reader.
- Keyboard coverage is the request dialog only; Requests filters/paging,
  Notifications, Settings and the wizard have not been tabbed through by hand.
- Mocked job-run, user and match-suggestion lists are empty, so those tables'
  scroll regions are not axe-checked with data. Login, error/empty states and the
  non-admin view are not audited.
- Only 360px and 1280px are tested.
- No Requests-page or request-dialog mockup comparison is written up yet
  (`01` and `02` in `references/mockups/final/`).

## Findings and fixes

| Finding | Fix |
| --- | --- |
| Primary button hover colour `#8d65ff` gave 3.86:1 with white text (WCAG AA needs 4.5:1) | Hover is now `#6a3de8` (`web/src/styles.css`) |
| Settings overflowed the viewport by 75px at 360px: `.table-wrap` had no styles and grid children could not shrink below table width | `.shell > * { min-width: 0 }` and `.table-wrap { overflow-x: auto }` |
| Scrollable tables were not keyboard reachable (axe `scrollable-region-focusable`) | Each `.table-wrap` is a labelled, focusable `region` |
| Request dialog did not take focus, let Tab leave it, ignored Escape and lost the opener's focus | `RequestModal` focuses itself on open, traps Tab, closes on Escape and restores focus |
| Settings page had two `<h1>` elements after the status panel was added | Original heading demoted to `<h2>` (found in review of ticket 23) |

## Differences from `references/mockups/final/`

Intentional:

- **Top navigation bar instead of a left sidebar.** It keeps content full width on
  phones without a collapsing drawer. Navigation is the same set of destinations.
- **No secret inputs in the wizard.** The Metadata Providers mockup shows API token
  and client secret fields with a reveal toggle. Requestarr shows read-only
  environment-variable references instead, so secrets never pass through the
  browser (AGENTS.md: never log or expose secrets; secrets live in the server
  environment).
- **Wizard steps are an ordered list** (`aria-current="step"`) rather than the
  mockup's connected-circle sidebar, which does not fit narrow screens.
- **Settings shows an operational status table** (Source, last sync, outcome,
  error) rather than the mockup's per-library cards with health, version and
  item counts: those values are not collected.

Not yet built (gaps, not decisions):

- Home has only the link box; the Recent Requests, Recently Available and Needs
  Review panels from the Home mockup are absent.
- Settings has no Test Connection / Edit / enable toggle per library, and no
  sub-navigation (Users & Access, Providers, Jobs, Logs). Test Connection exists
  only in the setup wizard.
- No notification badge count or activity page.
