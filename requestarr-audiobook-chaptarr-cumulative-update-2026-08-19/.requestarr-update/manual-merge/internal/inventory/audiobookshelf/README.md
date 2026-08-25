# Audiobookshelf inventory adapter

Implement behind `inventory.Source`. Audiobookshelf is the only authoritative audiobook source in V1. Synchronize normalized inventory; do not change request state directly.

Prefer exact ASIN/external IDs and retain author, narrator, series, source deep link, and raw identifiers needed for fallback reconciliation. Temporary errors, pagination failures, cancellation, or partial enumeration must retain the last-known successful inventory snapshot and must not emit mass removals.
