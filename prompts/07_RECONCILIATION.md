# Prompt 07 — Reconciliation

Implement targeted reconciliation from inventory change events to active requests. Prefer exact external IDs, preserve match reason/confidence, and define controlled fallback matching. Movie/TV availability uses enabled Plex/Jellyfin sources; music uses enabled Plex/Jellyfin/Navidrome; games use RomM. A game request is satisfied when any selected acceptable platform is present; Any accepts any platform. Make transitions idempotent and auditable. Add partial TV/music behavior only where the documented request model supports it.
