# Requestarr — SignalLuxe redesign implementation handoff

## Purpose
Restyle the **existing Requestarr product** with the user's selected **SignalLuxe** dark, futuristic visual direction. Preserve the existing request-only mission: browse media already hosted, search external metadata, request missing items, manage requests/issues, sync integrations, and administer the app. **No acquisition/download functionality is in scope.**

## Start here
1. Read `agent/IMPLEMENTATION_BRIEF.md`, `specs/PRODUCT_TRUTH_AND_GAPS.md`, `design-system/DESIGN_LANGUAGE.md`.
2. Compare `reference-current/` against `mockups/`. Current screenshots are the stronger evidence for existing behavior; generated mockups are visual direction only.
3. Read `specs/SCREENS.md`, `specs/COMPONENTS_AND_INTERACTIONS.md`, `specs/ACCESSIBILITY_RESPONSIVE.md` and `specs/ACCEPTANCE_CHECKLIST.md`.
4. Inspect the application source, routing, authorization, data models, APIs, components and CSS conventions **before making changes**. Preserve existing business logic and permission gates.
5. Implement a reusable shell and theme tokens, then migrate the pages incrementally. Validate each route in desktop and mobile.

## Contents
- `mockups/`: six requested page mockups, original SignalLuxe concept and supplemental generated design board
- `reference-current/`: five user-supplied original screenshots
- `assets/`: editable SVG background and a starter brand emblem
- `design-system/`: design language, token JSON, starter CSS
- `specs/`: screen-by-screen requirements, components, accessibility, non-goals and QA
- `agent/`: agent instruction / workflow

## Cautions
- Concept images are **not pixel-perfect specifications**. Some text/content and workflows are AI-invented. The generated Logs screen mentions Radarr/Sonarr/downloading: **do not implement these integrations**. Logs should show Requestarr's own operational events and its actual configured services only.
- The mockups show a New Request button, extra statistics, search affordances, and dynamic states that may need backend support. Do not imply they work without implementing and testing them or deliberately hiding them.
- The mockups include Settings > Logs and a separate primary Logs nav entry; keep the single primary Logs route; any Settings link should navigate there rather than create duplicate pages.
- The six individual screens do not include a separately generated Notifications screen. Its requirements are specified in `specs/SCREENS.md`.
- No application source files were supplied for this package. API, component and route names are proposals and must be reconciled with the repository.

## Suggested sequence
Foundation → app shell/navigation → shared media cards → Request → Library → Requests → Issues → Notifications → Settings → Logs → polish/QA.
