package reconcile

// Package reconcile will compare normalized inventory changes against active
// requests. It should resolve only requests affected by the changed canonical IDs
// or fallback match keys. A game request is satisfied when ANY selected acceptable
// platform is present; `Any` accepts any platform for the canonical game.
