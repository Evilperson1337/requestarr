# Requestarr

A shared request portal where people ask for movies, TV, music and games, and are told when a library has them.

## Language

**Request**:
The single shared ask for one canonical catalog item. Every user who wants the item attaches to the same Request.
_Avoid_: Ticket, vote, order

**Requester**:
The user who first created a Request; subscribed to it automatically.
_Avoid_: Owner

**Subscriber**:
A user who gets notified about a Request, either as its Requester or by choosing Notify Me.
_Avoid_: Follower, voter

**Viewer**:
A signed-in user who can browse but cannot create Requests. The default role for a new user.
_Avoid_: Guest, read-only user

**Catalog item**:
The canonical identity of a movie, show, album, song or game, resolved from a pasted provider URL.
_Avoid_: Media, title

**Source**:
A configured library (Plex, Jellyfin, Navidrome, RomM) that is authoritative for specific media types.
_Avoid_: Server, integration

**Availability**:
Whether a catalog item is present in at least one Source. It becomes missing only when every Source that covers the item has completed a successful sync without it. A failed sync never counts as absence.
_Avoid_: Status, in stock

**Request status**:
Where a Request is in its life: requested, approved, denied, available or cancelled. `available` is a projection of the item's Availability, set in the same step that Availability becomes true.
_Avoid_: State, stage

**Cancellation**:
A Requester leaving a Request. The Request becomes `cancelled` only if they were the last Subscriber; otherwise they just unsubscribe. An admin can cancel any Request outright.
_Avoid_: Delete, withdraw

**Alias**:
An alternate identifier recorded for a catalog item so a Source's identifiers can be matched to it. Plex exposes MusicBrainz release and release-track ids, while an album Request is identified by its release group and a song by its recording, so those ids are stored as aliases of the item (D020).
_Avoid_: Synonym, duplicate ID
