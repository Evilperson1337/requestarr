# Component inventory and interactions

## Suggested component hierarchy (adapt to project's framework)
`AppShell` > `SidebarNav` / `MobileNav` / `UserMenu` / `PageHeader` / `PageContent`.
`PageContent` uses `AmbientBackground`, `SurfacePanel`, `SectionHeader`, `MediaSectionRail`, `MediaCard`, `MediaGrid`, `MediaTypeTabs`, `SearchField`, `FilterChipGroup`, `StatusBadge`, `IntegrationCard`, `RequestRow`, `IssueRow`, `LogTable`, `EmptyState`, `LoadingSkeleton`, `ErrorNotice`, `ConfirmDialog`, `Toast`.

## Behaviors
- Sidebar active indicator reflects actual route, supports keyboard, full label/tooltip on collapse, and all seven nav items including Logs.
- Tabs/chips: exclusive selection unless multi-select explicitly supported; show selected state with semantics `aria-selected` or pressed as appropriate.
- Search: Enter submits or triggers preview based on current product behavior; escape clears only when focused and discoverable. Debounced remote querying only where current API supports live searches.
- Carousel: buttons accessible, scroll-snap, mouse/touch/keyboard, preserve art size; no unexpected auto-scroll.
- Media card: title and metadata legible, semantic link/button, focus state, artwork alt or decorative image depending surrounding text, fallback image.
- Request action row: status-dependent allowed actions; loading/disabled state; do not optimistically claim success on failure.
- Integration card: distinct unconfigured, configured, syncing, success, warning, failed states; last successful run only when known.
- Logs: monospace optional only for timestamps and technical data; wrap message safely; severity and timestamps accessible; no sensitive strings rendered.

## Component contract guidelines
Components accept data and callbacks, not hardcoded service names/request counts. Theme uses CSS custom props and semantic intent variants (not raw magenta prop on every instance). Keep data fetching and mutations in existing hooks/services. Reuse existing accessible primitives, if any, before building new ones.

## State matrix to test
Default; loading; empty; network error; filtered zero results; unauthorized; long title; missing artwork; multiple request statuses; one vs many results; 100+ items; time locale/zone; narrow/mobile. Always test visual focus and hover states.
