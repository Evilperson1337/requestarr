# Plex inventory adapter

Implements `inventory.Source` for Plex movie and TV libraries (`client.go`). It
reads only: it never changes request status, and any failure is an error with no
items, so a failed sync keeps last-known inventory and removes nothing (D015).
The shared contract suite (`internal/inventory/inventorytest`) runs over the
recorded fixtures in `testdata/`; see `docs/INTEGRATIONS.md` for configuration.
