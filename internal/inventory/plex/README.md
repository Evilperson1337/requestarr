# Plex inventory adapter

Implements `inventory.Source` for Plex movie, TV and music libraries
(`client.go`). It reads only: it never changes request status, and any failure is
an error with no items, so a failed sync keeps last-known inventory and removes
nothing (D015). Music libraries are read as albums (Plex type 9) and tracks
(type 10); their MusicBrainz ids are bridged to catalog identity by aliases (see
`docs/INTEGRATIONS.md`). The shared contract suite
(`internal/inventory/inventorytest`) runs over the recorded fixtures in
`testdata/`.
