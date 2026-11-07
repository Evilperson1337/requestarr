# Requestarr

A shared request portal where people ask for movies, TV, music, games and audiobooks, and are told when a library has them.

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

**Superuser**:
The owner role, ranked above Admin, with nothing withheld. Only a Superuser can give or take the Admin and Superuser roles. Every admin became a Superuser when the role was introduced, and the instance always keeps at least one Superuser who can sign in (the Local administrator counts only while enabled).
_Avoid_: Owner, root, super admin

**Admin**:
The role that runs Requestarr day to day: approves, denies and cancels Requests, handles Issues and matches, and moves people between Requester and Viewer. An Admin cannot change an Admin or a Superuser, and cannot give either role.
_Avoid_: Administrator (as a role; the Local administrator is an account), moderator

**Viewer**:
A signed-in user who can browse but cannot create Requests. The default role for a new user.
_Avoid_: Guest, read-only user

**Catalog item**:
The canonical identity of a movie, show, album, song, game or audiobook, resolved from a provider URL, whether pasted or picked from a Search result (an audiobook may be given as a bare ASIN; the ASIN is its identity).
_Avoid_: Media, title

**Search result**:
A candidate found by searching a provider by name. It points at a Catalog item without being one until it is resolved; picking it resolves it exactly as a pasted link would.
_Avoid_: Result, hit

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

**Stored secret**:
An integration credential an admin entered in Setup and Requestarr keeps encrypted in its database, as opposed to one supplied by the environment. It can be replaced or cleared but never read back.
_Avoid_: Saved password, vaulted credential

**Secret source**:
Where the value of a secret in use comes from: the environment, or a Stored secret. The environment wins when both exist, and the Stored secret is then shadowed.
_Avoid_: Origin, provider

**Master key**:
The operator-supplied key that encrypts Stored secrets. It lives only in the environment, never in the database.
_Avoid_: Encryption password, root secret

**Source kind**:
The type of a Source: Jellyfin, Plex, Navidrome or RomM. A kind fixes the slug, source ID, media types, Credential fields and pairing rule; a Source is one configured instance of it.
_Avoid_: Source type, provider, backend

**Credential field**:
One input a Source kind or integration needs to connect: a URL, a token, or a username and password. Each is either plain or a Stored secret, and names the environment variable that can supply it.
_Avoid_: Setting, auth param

**Sync health**:
The single read model of a Source's last run outcome (from the job runs) and its last successful snapshot. A failed run never changes what the last successful snapshot says.
_Avoid_: Sync status, last sync

**Library item**:
Something a Source actually holds, as of its latest successful sync: a movie, show, album, song, game or audiobook. Most Library items have never been requested and have no Catalog item. A Source's Library items are browsable, and an Issue is about one.
_Avoid_: Inventory row, media file, title

**Issue**:
A report that a Library item has a problem, such as broken subtitles or a game that will not boot. It is separate from a Request: a Request asks for something no Source has, an Issue is about something one does. Requestarr only records it and tells people; an admin fixes the item outside Requestarr and resolves the Issue. It never causes any download or replacement.
_Avoid_: Ticket, bug, complaint

**Issue kind**:
The category of an Issue, such as video, audio, subtitles, wrong region or does not boot. The kinds offered depend on the item's media type.
_Avoid_: Issue type, severity

**Private host**:
A Source address that is, by its form alone, on a local or private network: a private or loopback IP, a single-label name, or a name ending in a local suffix the operator allows. Only a Source on a private host may be reached without https.
_Avoid_: Local server, LAN address

**Local administrator**:
The single built-in account that signs in with a password instead of through OIDC. It exists so a new instance can be set up, and recovered when OIDC is misconfigured, without an identity provider. It is the only account with a local password and always has the Superuser role, which cannot be changed.
_Avoid_: Root, default user, local user
