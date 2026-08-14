# Plex inventory adapter

Implement behind `inventory.Source`. The adapter updates Requestarr's normalized inventory; it never changes request status directly. A reconciliation service consumes inventory change events. Temporary source failures must retain last-known inventory rather than emitting mass removals.
