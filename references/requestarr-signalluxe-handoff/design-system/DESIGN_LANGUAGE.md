# SignalLuxe — design language

## Visual identity
A premium, futuristic **dark-only** media interface: midnight navy canvas, restrained luminous magenta/violet edges, occasional cool-blue contrast, comfortable reading surfaces, and prominent media art. Atmosphere supports content instead of competing with it. Unlike literal concept art, implement real hierarchy, predictable behavior, and accessible contrast.

### Colors
See `tokens.json` / `tokens.css` for starter palette. Tokens were **approximated** from the imagery, not sampled/approved. Use primary text `#F4F4FC`, muted metadata `#B7BAD2`; deep navy panels; pink for selection/primary CTA, teal/blue for informational accents. Use amber warning, red error, green success, not pink for all status states. Do not communicate status by color alone.

### Backgrounds and glow
- Single ambient page background in `assets/signalluxe-background.svg`; apply a dark overlay above it on text-heavy views.
- Do not reproduce the bright pink planet across all available text area at full opacity. Maintain strong contrast across positions.
- Thin borders and low-opacity glows for active nav/primary action; most content panels use non-glowing boundaries.
- Avoid continuous animated glows; only short hover/focus transitions. Honor reduced motion.

### Typography
Inter (if licensed/available) with robust system fallback; no font binaries bundled. Page titles approximately 30–36px desktop; section titles 18–22px; UI 14–16px; captions 12–13px. Favor clarity over stylized tracking. Long media titles may wrap to two lines; preserve full title in accessible text/tooltip.

### Spacing, shapes and grids
4px base scale. Main sidebar 248–272px; 24–32px desktop content padding; cards 14–18px radius, controls 10–12px, chips fully rounded. Constrain form-dense pages to comfortable max width; media library can use full remaining width. Equal card widths within rows; consistent art ratios **by media category**, not one forced size for all.

### Media art preservation
- Movies/TV/audiobooks: tall poster/book-cover treatments with consistent fixed ratio as available.
- Albums: square covers, as in current UI; don't stretch into movie posters.
- Games: show the native source artwork aspect ratio or a consistent object-fit box per game collection; do not crop essential text.
- Always `object-fit: cover` only where appropriate, with alternative `contain` for letterboxed/landscape artwork. Keep category labels/title/source/year below artwork. Provide fallbacks.
- Horizontal carousels on Request home; dense responsive poster grid in Library; compact thumbnail + details rows in requests/issues.

### Interaction language
Request/search main action has a single obvious next step. Filters are discoverable chips; selected state uses background + border + optionally icon, never only glow. Status badges convey actual workflow state. Skeletons for loading; descriptive empty states and retry on failure. Confirmation for destructive or irreversible actions.

### Component styling baseline
- Sidebar: branded header, route icons, count badges, active row, profile/logout area. Include **Logs** at primary nav level.
- Primary CTA: pink-magenta gradient with dark/white text chosen to pass contrast; hover brightens; keyboard focus uses cyan outline.
- Inputs: dark elevated surface with 1px border, focus ring, leading icons sparingly. Labels must remain visible even with placeholder.
- Panels: navy surface, soft stroke, limited shadows, avoid nesting panels five deep.
- Tables: sticky heading only when needed, high-contrast body text, useful row height.

### Responsive
Desktop: permanent sidebar. Tablet: collapsible sidebar with label-preserving expansion. Mobile: drawer navigation with discoverable toggle, one-column management cards, 2–3 art columns depending width. Preserve search, status and actionable controls without horizontal overflow.
