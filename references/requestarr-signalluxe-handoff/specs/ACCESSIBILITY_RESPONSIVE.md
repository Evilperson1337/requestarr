# Accessibility / performance / responsiveness
- Target WCAG 2.2 AA for text contrast, controls, focus indication, touch targets and keyboard operation. Recalculate actual contrast on the layered SVG backdrop.
- Keep text sizes comfortable; 13–16px for substantive UI, captions not below 12px. Decorative glow and background remain outside content hierarchy.
- All icons accompanying navigation/actions have visible labels or accessible names. Notifications and statuses not conveyed solely through hue.
- Modal/dialog: label, focus trap, escape behavior, return focus, action confirmation as appropriate.
- Honor reduced motion; avoid motion-heavy gradients, animated stars and auto-advancing carousels.
- Layout breakpoints guidance: >=1200 desktop sidebar; 768–1199 narrow/collapsible sidebar; <768 mobile drawer. Decide exact cutoffs after testing. Don't lock card count to 7 or 8 per row.
- Image loading: srcset/responsive poster sizes, lazy-load beyond viewport, stable aspect-ratio boxes to avoid CLS, fallbacks. Never use screenshot-cropped poster art as real media assets.
- Rendering: pagination/virtualization for large libraries or logs; avoid fetch-all with unlimited art or live log streaming without retention/buffer limit.
- Security: no auth tokens in client logs; role-check all actions on server; escape log strings; no secrets exposed in integration cards.
