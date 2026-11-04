package igdb

import "time"

// MaxSearchesInFlight is the concurrency cap, for tests.
const MaxSearchesInFlight = maxSearchesInFlight

// SetSearchWait shortens how long a search waits for a free slot.
func (r *Resolver) SetSearchWait(d time.Duration) { r.searchWait = d }
