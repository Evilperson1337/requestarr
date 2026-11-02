package metadata

import "errors"

// Resolver errors carry one of these when the cause is something the user or an
// administrator can act on; anything else is a bad link or a missing item.
var (
	// ErrBusy: the provider is throttling or briefly down. Trying again later may work.
	ErrBusy = errors.New("metadata provider is busy")
	// ErrCredentials: the provider rejected Requestarr's credentials.
	ErrCredentials = errors.New("metadata provider rejected the credentials")
	// ErrNotFound: the provider answered, and has no such item.
	ErrNotFound = errors.New("metadata provider has no such item")
	// ErrNotConfigured: this kind of link needs credentials that are not set.
	ErrNotConfigured = errors.New("metadata provider is not configured")
)
