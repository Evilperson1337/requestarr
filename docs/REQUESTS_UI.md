# Using the shared Requests page

Open **Requests** from the main navigation, or bookmark `/requests` directly.
All authenticated users see the same shared queue. The app uses real URLs for
navigation, reloads, copied links, and browser Back/Forward.

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
- Only admins see **Approve**, **Deny**, and **Cancel request** where permitted
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
