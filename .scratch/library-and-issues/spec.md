# Library browser and Issues

Status: ready-for-agent

Let people browse what the libraries hold and report a problem with an item, as Seerr does. Decisions come from a grilling session; domain terms (**Library item**, **Issue**, **Issue kind**) are in `GLOSSARY.md`, the decision is D030.

## Boundary

An Issue is a report about something a Source already holds. Requestarr stores it and tells people; an admin fixes the item outside Requestarr and resolves the Issue. It never triggers a download, a replacement or any *arr call (AGENTS.md boundary, V1_SCOPE).

## Decisions

1. **Issue is not a Request.** Separate table, separate lifecycle, separate list.
2. **Library page.** A sidebar entry with tabs Movies, TV, Music, Games. Each tab uses its own cover shape (posters 2:3, albums 1:1, games 3:4). Title search, Source filter, newest-added ordering (the library's own added date, else first sighting). Songs are browsed under their album, never as 30k rows.
3. **An Issue attaches to the Library item** (the thing that is broken), with an optional Catalog item link when one is known.
4. **Who may report:** every signed-in role, Viewers included. Only admins resolve, close or reopen. A reporter may comment and leave. Each user is limited to a handful of new reports an hour.
5. **Kinds, fixed in code per media type, "Other" last and always requiring a description:**
   - Movie, series: Video, Audio, Subtitles, Wrong item, Other
   - Album, song: Audio, Missing or wrong tracks, Wrong tags or artwork, Other
   - Game: Doesn't boot or launch, Wrong region, Wrong version or platform, Missing files, Other
6. **Contents:** kind, description (at most 2,000 characters), and an optional **part** reusing the Selection type (season or episode of a series, track of an album). Movies and games have no part.
7. **One shared Issue** per open (Library item, kind, part). A second reporter of the same thing is added to it as a reporter with their note; there is no vote count. A different kind or part opens a separate Issue.
8. **Statuses:** `open`, `resolved`, `closed` (won't fix). An admin moves them and may add a closing comment; reopening is allowed.
9. **Notifications:**
   - Admins: a new Issue goes to in-app notifications and the global Apprise route.
   - Reporters: resolve, close, reopen and each admin comment, in-app and Apprise. A reporter may leave an Issue to stop them.
10. **Item leaves its library:** the Issue stays open and is labelled "no longer in the library"; an admin resolves it. The label clears if the item reappears. Nothing is auto-resolved.
11. **Navigation:** Library and Issues in the sidebar for everyone. Admins see all Issues, others their own. A count badge appears only for admins (open Issues).
12. **List behaviour:** the Issues list reuses the Requests list's filters (status, kind, media type), newest-first and snapshot pagination.

## Out of scope

Automatic resolution, SLA or severity levels, attachments or screenshots, per-Source admin routing, a Library page for Viewers that hides items by role.

## Build order

See `issues/`. Tickets 01-03 can start together; 04 and 05 depend on 01.
