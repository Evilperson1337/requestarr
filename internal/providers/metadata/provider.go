package metadata

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sync"
	"time"

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
	now       func() time.Time
	mu        sync.Mutex
	cache     map[string]cacheEntry
}

type cacheEntry struct {
	item domain.ResolvedItem
	at   time.Time
}

const (
	// cacheFresh is how long a resolved link is served without asking the
	// provider again.
	cacheFresh = 10 * time.Minute
	// cacheStale is how long an old answer may stand in when the provider is busy.
	cacheStale = 24 * time.Hour
	cacheLimit = 256
)

func NewService(catalog Catalog, resolvers ...Resolver) *Service {
	return &Service{resolvers: append([]Resolver(nil), resolvers...), catalog: catalog, now: time.Now, cache: map[string]cacheEntry{}}
}

func (s *Service) Resolve(ctx context.Context, raw string) (domain.ResolvedItem, error) {
	u, err := ParseURL(raw)
	if err != nil {
		return domain.ResolvedItem{}, err
	}
	key := u.String()
	if item, ok := s.lookup(key, cacheFresh); ok {
		return item, nil
	}
	for _, resolver := range s.resolvers {
		if !resolver.CanResolve(u) {
			continue
		}
		item, err := resolver.Resolve(ctx, u)
		if err != nil {
			// A busy provider is not the user's problem while an older answer exists.
			if errors.Is(err, ErrBusy) {
				if old, ok := s.lookup(key, cacheStale); ok {
					return old, nil
				}
			}
			return domain.ResolvedItem{}, err
		}
		s.store(key, item)
		return item, nil
	}
	return domain.ResolvedItem{}, fmt.Errorf("unsupported metadata URL")
}

func (s *Service) lookup(key string, maxAge time.Duration) (domain.ResolvedItem, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.cache[key]
	if !ok || s.now().Sub(entry.at) > maxAge {
		return domain.ResolvedItem{}, false
	}
	return entry.item, true
}

func (s *Service) store(key string, item domain.ResolvedItem) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.cache) >= cacheLimit {
		if _, replacing := s.cache[key]; !replacing {
			oldest, oldestAt := "", s.now()
			for k, e := range s.cache {
				if oldest == "" || e.at.Before(oldestAt) {
					oldest, oldestAt = k, e.at
				}
			}
			delete(s.cache, oldest)
		}
	}
	s.cache[key] = cacheEntry{item: item, at: s.now()}
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
