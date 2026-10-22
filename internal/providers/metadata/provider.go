package metadata

import (
	"context"
	"fmt"
	"net/url"

	"github.com/evilperson1337/requestarr/internal/domain"
)

// Resolver recognizes a provider URL and resolves it into Requestarr's
// canonical domain representation. Implementations must not create requests.
type Resolver interface {
	Name() string
	CanResolve(*url.URL) bool
	Resolve(context.Context, *url.URL) (domain.ResolvedItem, error)
}

type Catalog interface {
	Upsert(context.Context, domain.ResolvedItem) (domain.CatalogItem, error)
}

type Service struct {
	resolvers []Resolver
	catalog   Catalog
}

func NewService(catalog Catalog, resolvers ...Resolver) *Service {
	return &Service{resolvers: append([]Resolver(nil), resolvers...), catalog: catalog}
}

func (s *Service) Resolve(ctx context.Context, raw string) (domain.ResolvedItem, error) {
	u, err := ParseURL(raw)
	if err != nil {
		return domain.ResolvedItem{}, err
	}
	for _, resolver := range s.resolvers {
		if resolver.CanResolve(u) {
			item, err := resolver.Resolve(ctx, u)
			if err != nil {
				return domain.ResolvedItem{}, err
			}
			return item, nil
		}
	}
	return domain.ResolvedItem{}, fmt.Errorf("unsupported metadata URL")
}

func (s *Service) ResolveAndUpsert(ctx context.Context, raw string) (domain.ResolvedItem, error) {
	item, err := s.Resolve(ctx, raw)
	if err != nil {
		return domain.ResolvedItem{}, err
	}
	item.Item, err = s.catalog.Upsert(ctx, item)
	if err != nil {
		return domain.ResolvedItem{}, fmt.Errorf("save catalog item: %w", err)
	}
	return item, nil
}
