# Implementation brief for coding agent

You are modifying the **existing** Requestarr codebase to deliver a cohesive dark-mode SignalLuxe UI inspired by the supplied mockups. Do not generate a greenfield app unless requested. This is a **presentation/UI/UX** project, not a redesign of the data model or a downloader.

## Before coding: audit
1. Identify framework, frontend build system, routes, existing components, CSS conventions, testing, localization, auth/role permission model and backend endpoints.
2. Map existing features to each route in `specs/SCREENS.md`, noting missing APIs or ambiguous actions. Document what is confirmed vs inferred.
3. Take a baseline screenshot at key breakpoints and review flows on the original app.
4. Propose implementation phases and file changes before broad refactors. Keep backward compatibility as default.

## Execute in vertical slices
**Slice A**: Implement CSS tokens, atmospheric background, app shell, navigation (including Logs), mobile drawer, reusable typography and surface primitives. Do not break route guards.
**Slice B**: Media cards, image fallbacks, rails/grids and Request home; preserve lookup/preview semantics.
**Slice C**: Library filters/art ratios/loads and Requests status/action handling.
**Slice D**: Issues and Notifications, honoring current permissions and domain state machine.
**Slice E**: Settings integration cards and form layouts. Never expose secrets.
**Slice F**: Logs route and safe read-only log endpoint/filtering if warranted by repo. Do not implement fake 'live' behaviors; no acquisition integrations.
**Slice G**: Responsive/a11y review, test update, screenshot regression, docs.

## Source of truth priority
1. Existing application's domain logic/API permissions.
2. Existing user-supplied screenshots in `reference-current/`.
3. `specs/PRODUCT_TRUTH_AND_GAPS.md` and written constraints.
4. New mockup visuals as aesthetic targets, not factual behavior.

## Agent output artifacts
Return architecture audit, an implementation plan, exact files changed, screenshot comparisons, tests run and remaining known gaps. For any unsupported feature pictured in a mockup, flag it and either adapt safely or defer rather than simulate success.
