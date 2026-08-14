package metadata

import (
	"context"
	"net/url"

	"github.com/example/requestarr/internal/domain"
)

// Resolver recognizes a provider URL and resolves it into Requestarr's
// canonical domain representation. Implementations must not create requests.
type Resolver interface {
	Name() string
	CanResolve(*url.URL) bool
	Resolve(context.Context, *url.URL) (domain.ResolvedItem, error)
}
