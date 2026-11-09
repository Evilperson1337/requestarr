# Screen-by-screen requirements

## Shared shell / navigation
`Request`, `Library`, `Requests`, `Issues`, `Notifications`, `Settings`, `Logs` in that order. Active route highlighted; count badges use real server state; user/admin context at bottom. Keep Settings subtabs as existing if backend supports them; the dedicated Logs nav item points to one canonical log route. Global site search is **not** implied; Request search remains on Request unless the source supports universal search.

## 1 — Request home (`mockups/request-home.png`)
Prominent title/search form, title-or-link input, media type selector, include singles/EPs when relevant, preview/search action. Preserve actual semantics (current screenshot notes album searches require submit rather than live typeahead); do not silently change trigger policy. Carousels: recent requests, new movies/TV, new music, new games, new audiobooks. Small contextual empty state for zero requests. Search result/preview flow needs explicit fidelity to backend.

## 2 — Library (`mockups/library.png`)
Tabs for Movies, TV, Music, Games, Audiobooks. Search title, filter by connected library, optional supported sort and density selector. Keep posters and albums as source-appropriate art ratios; per-item title, type/year/source metadata. Responsive grid, pagination/load-more or virtualized scrolling chosen per current API. Empty/loading/error states.

## 3 — Requests (`mockups/requests.png`)
Statuses All/Requested/Approved/Available/Denied/Cancelled. Search, type filter, sort, My Requests, pagination. Item row with thumbnail, title, media type, requester, date, status and available authorized actions. Primary status action only when valid; prevent approval/denial from showing on completed/invalid state. Confirm destructive actions, surface network result and preserve filter state.

## 4 — Issues (`mockups/issues.png`)
Open/In progress/Resolved/Closed/All. Issue cards show affected title artwork, issue category, reporter count, reported date, state. Clicking opens details (if supported): description, reporters/comments, status history, controls for role-authorized triage. When list has one result, keep the content compact rather than an enormous decorative empty container. Report Issue CTA only if app already provides or agent implements authorized flow.

## 5 — Notifications (no standalone image; derive style from shared shell)
Unread/all or existing supported filters; reverse-chronological list of events with icon, time, linked request/issue title and read state. Mark read/mark all read only if supported. Read/unread must be distinguishable without color alone. Useful empty state and compact list. Respect per-user notification permissions; no invented push channels.

## 6 — Settings (`mockups/settings-integrations.png`)
Preserve subtabs Integrations, Authentication, Activity, Matches, Users, Logs where present; nav Logs links to canonical logs page. Group cards Media Libraries, Metadata, Notifications. Each card: real integration name/logo or fallback, configured/running/last success state, permitted Setup/Settings/Sync action. Never display token/secret values. Consider smaller subpanels so long explanatory text stays readable. Other tabs retain existing forms/validation.

## 7 — Logs (`mockups/logs.png`)
New top-level route. **Must describe Requestarr's own logs, not download/acquisition activity**. Search/filter by severity/service/time only if logs API supports it. Accessible table or list with time, component, severity, message; detail disclosure for structured metadata. Pause/stream, clear, copy/export only with permission and backend capability. Sanitize credentials, tokens, IP/PII where appropriate; cap rows and avoid logging sensitive request payloads. If no accessible logs endpoint exists, first implement a safe read-only paginated logs API with permissions or defer route behind an informative placeholder; don't fake live data.

## Additional flows not fully visualized
Request preview/detail, library media detail, issues detail, notifications, login/OIDC callback, integration setup modal/forms, empty/loading/error states. These are required for a coherent product but should be adapted to existing implementation before inventing new navigation.
