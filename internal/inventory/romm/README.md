# RomM inventory adapter

Implements `inventory.Source` for RomM (`client.go`). RomM is authoritative only for games. It reads only: it never changes request status, and any failure is an error with no items, so a failed sync keeps last-known inventory and removes nothing (D015). Each ROM carries its IGDB game id and a platform key derived from the platform's IGDB id; game Requests are satisfied by the OR rule over platforms (D021). The shared contract suite (`internal/inventory/inventorytest`) runs over the fixtures in `testdata/`; see `docs/INTEGRATIONS.md` for configuration.
