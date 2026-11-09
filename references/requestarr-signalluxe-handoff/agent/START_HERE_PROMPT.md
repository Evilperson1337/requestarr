# Copy this prompt into your coding agent

Implement the Requestarr SignalLuxe UI redesign from the attached `requestarr-signalluxe-handoff` package within this existing repository.

First inspect the repo and read the package README, `agent/IMPLEMENTATION_BRIEF.md`, `specs/PRODUCT_TRUTH_AND_GAPS.md`, `specs/SCREENS.md`, `design-system/DESIGN_LANGUAGE.md` and the acceptance checklist. Audit existing routes, API, access control, frontend stack and test setup. Produce a concise mapping of current components to intended screens and distinguish styling improvements from unsupported new capabilities.

Use the six full-screen PNG mockups as the visual guide, but preserve all confirmed backend behavior and keep album artwork square, films/TV/book art portrait, and games using appropriate source art. Add Logs to the primary left nav. Implement an accessible dark-only responsive design system using `design-system/tokens.json` and `tokens.css` as starters and `assets/signalluxe-background.svg` as editable backdrop. Keep glow restrained and text readable. Do not add any acquisition/downloader services or fake data pictured in illustrative mockups.

Work incrementally in the order of the handoff brief. Show test evidence and a list of implementation decisions/deviations. Preserve existing data and account settings. If an action needs a backend capability not yet present, document the gap rather than creating a cosmetic nonworking UI.
