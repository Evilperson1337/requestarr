# Using the shared Requests page

Open **Requests** from the main navigation, or bookmark `/requests` directly.
All authenticated users see the same shared queue. The app uses real URLs for
navigation, reloads, copied links, and browser Back/Forward.

## Pasting TMDB links

Movie and TV links copied directly from TMDB can include the readable title
after the numeric ID. For example,
https://www.themoviedb.org/movie/1762506-band-of-brothers-legacy and
https://www.themoviedb.org/movie/1762506 resolve to the same catalog item.
There is no need to remove the title suffix; it does not create a separate
Request. Provider credentials and connectivity must still be configured.

## Movie and TV confirmation previews

Movie and TV links open a wider confirmation modal on desktop and a full-screen,
stacked modal on phones. The complete poster retains its aspect ratio; a
restrained optional backdrop sits above readable title and details. The preview
uses a dark scrollbar track and purple thumb, deferring to system colors in
high-contrast mode. The preview
shows runtime, genres, locale-formatted release date/status, a TMDB score and its
rating count, and an official synopsis with Show more / Show less. Director names
and up to five lead cast members appear when supplied.

TV uses first/last air dates, series status, per-episode runtime, season/episode
counts, and series creators rather than a movie release date or director.
**Request series** requests the whole series by default. Choosing "Choose seasons or
episodes" lists each season (and, when TMDB supplied them, its episodes); a whole
season absorbs its episodes. Albums work the same way with a tracklist ("Choose
tracks"). The picker is hidden when the provider supplied no list, so the whole item
is always requestable. When the shared Request is open and covers only part of the
item, anyone allowed to request sees **Add to request**; parts already requested are
listed but disabled, and adding never narrows the Request (D027). The Requests list
says what a partial Request covers. Ended/canceled status does not imply library
availability, and an upcoming series remains requestable under the usual policy.

Missing facts are not fabricated: unrated movies say Not yet rated, missing dates
stay unknown, and missing/broken artwork has a neutral fallback. A failed optional
credits lookup shows Some details unavailable without blocking the Request.
Upcoming movies remain requestable; release date is not library availability.

The action footer remains reachable while details scroll:

- New movies/series offer **Request movie** / **Request series** to requesters/admins and an expandable
  **Add a note**. Viewers cannot create Requests.
- Already requested movies show their shared lifecycle state, **Notify Me** or
  **Notifications On**, and **View request**. Closed Requests cannot gain new
  subscriptions.
- Known available movies show library-authoritative availability and **Open in
  library** links only when a usable, non-credential-bearing link exists. Unknown
  availability is not presented as missing.
- A successful submission stays in the modal with confirmation, subscription
  status, **View request**, and **Close**. A concurrent existing Request is reused
  without falsely claiming that a second one or a subscription was created.
- Submission errors preserve the note for retry. Authorization loss removes
  protected actions. Cancel/Close and Escape return focus to the initiating control.

Reopening the same movie or TV series in the current Home flow reuses resolved metadata and
refreshes local Request/availability state only. Expanding synopsis or note does
not call TMDB. A fresh explicit lookup can update the persisted metadata.
Music and game-specific previews keep their previous controls and behavior.

**View request** opens the shared queue filtered by the exact Request ID, not a
potentially ambiguous title search. Clear filters returns to the full queue.

## Finding requests

- **Search titles:** enter a literal title substring, then press Enter or Search.
- **Status:** choose All, Requested, Approved, Available, Denied, or Cancelled.
- **Media type:** choose a movie, TV, album, song, or game filter.
- **My Requests:** limits the shared queue to your subscriptions, including
  requests you created and other requests you follow with Notify Me.
- **Sort by:** newest first, oldest first, or lifecycle status.
- **Requests per page:** choose 25, 50, or 100. Bookmarked API-supported sizes
  from 1–100 are also reflected by the control.
- **Clear filters:** returns to the unfiltered shared queue.

The applied search, filters, sort, page size, and cursor are stored in the URL.
Changing a filter or ordering starts from the first page. Draft search text is
not applied until submitted. There is no separate private My Requests page.

## Moving between pages

Use **Next page**, **Previous page**, and **First page**. Previous page follows
the current browser-history trail; a later-page bookmark opened in a new tab
still offers First page even when there is no previous-page history. Browser
Back/Forward restore the corresponding URL and controls. Page navigation moves
keyboard focus to the Requests heading after loading.

Returning to a traversal's original first page restores its last-loaded rows
from that browser-history entry rather than mixing in a new first page. The UI
labels this restored view; Refresh view obtains fresh details and membership.
The history entry is user-scoped and expires after 15 minutes. Row actions
update those restored rows without discarding their membership.

The API does not supply a total page count, so the UI reports how many requests
are on this page and when the end is reached rather than inventing totals.
Membership/order stay fixed for up to 15 minutes while details remain current.
**Refresh view** restarts from the first page with the same filters. It includes
newly matching requests. Changes made through row actions refresh current page
details, not the active snapshot's membership/order.

Expired, evicted, or invalid cursors show an actionable message and **Restart
from first page**, preserving the other filters. Cursors are user-specific:
when sharing a view with another user, share its filters without the cursor or
let the recipient restart from the first page. See the [API guide](REQUESTS_API.md)
for snapshot limits and error codes.

## Subscribing, leaving, and cancelling

- Any authenticated user, including a viewer, can **Notify Me** on an open
  request they do not follow.
- Subscribers can **Leave request**, including after a request closes. A
  requester leaves through the same action; it is not a cancel-for-everyone
  shortcut.
- If leaving would cancel an open request because you are its last subscriber,
  the server asks for explicit confirmation. Declining leaves it unchanged.
  With other subscribers, leaving only removes your subscription.
- Only admins and superusers see **Approve**, **Deny**, and **Cancel request** where permitted
  by the lifecycle. Admin cancellation explicitly confirms that it affects
  everyone. Closed requests cannot be cancelled again.

Pending row actions are guarded against duplicate clicks. Action errors remain
visible, including request IDs, rather than being erased by a list refresh.
For a version conflict, use Refresh view to obtain current state before retrying.
The server remains authoritative for authorization and concurrency.

## Accessibility and artwork

Controls have labels and native keyboard operation, selected status chips use
pressed state, and page/loading/error messages use status or alert semantics.
Controls and actions wrap at phone widths; long titles do not force horizontal
scrolling. Poster and square artwork use distinct contained bounds, never a
destructive crop. Platform labels use the API's actual selection fields.
