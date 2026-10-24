# 26: Accessibility and responsive audit

**What to build:** Pages are accessible and usable at phone widths, compared against the final mockups and UI guidance.

**Blocked by:** 18 Requests page controls, 22 Wizard Notifications, Request Policy, Review and Finish, 23 Operational status view

**Status:** ready-for-human

- [ ] Keyboard and screen-reader checks pass on the main pages (automated axe on every page plus a dialog keyboard test are done; the manual screen-reader pass and full keyboard walk-through of each page remain, see docs/ACCESSIBILITY_AUDIT.md)
- [x] Layouts work at small widths without horizontal scroll
- [x] Differences from references/mockups/final are intentional and noted; accessibility wins over pixel matching
- [x] Findings and fixes are listed in docs/
