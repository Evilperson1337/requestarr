package metadata

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/evilperson1337/requestarr/internal/domain"
)

// SearchQuery is validated, trimmed user text for one provider.
type SearchQuery struct {
	Text string
	Page int
}

// Searcher finds candidate items by name. It never writes and never creates
// requests; it is deliberately not a Resolver. Every result's Ref must be a
// value the same provider's Resolver accepts.
type Searcher interface {
	Search(context.Context, SearchQuery) (domain.SearchPage, error)
}

const (
	// searchFresh is how long an answer is served without asking the provider again.
	searchFresh = 75 * time.Second
	// searchStale is how long an old answer may stand in when the provider is busy or down.
	searchStale = 6 * time.Hour
	searchLimit = 256
	// searchBudget bounds one shared provider call.
	searchBudget = 10 * time.Second
)

// SearchService routes a search to the provider for one search type. Answers
// are cached in memory only (never persisted) and identical concurrent
// searches share one provider call.
type SearchService struct {
	searchers map[string]Searcher
	mu        sync.Mutex
	now       func() time.Time
	cache     map[string]searchEntry
	flights   map[string]*searchFlight
}

type searchEntry struct {
	page domain.SearchPage
	at   time.Time
}

type searchFlight struct {
	done chan struct{}
	page domain.SearchPage
	err  error
}

func NewSearchService(searchers map[string]Searcher) *SearchService {
	copied := make(map[string]Searcher, len(searchers))
	for k, v := range searchers {
		copied[k] = v
	}
	return &SearchService{searchers: copied, now: time.Now, cache: map[string]searchEntry{}, flights: map[string]*searchFlight{}}
}

// SetClock replaces the clock the cache ages entries by. For tests.
func (s *SearchService) SetClock(now func() time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.now = now
}

// Types lists the search types whose provider is configured, sorted.
func (s *SearchService) Types() []string {
	types := make([]string, 0, len(s.searchers))
	for k := range s.searchers {
		types = append(types, k)
	}
	sort.Strings(types)
	return types
}

func (s *SearchService) Search(ctx context.Context, searchType, text string, page int) (domain.SearchPage, error) {
	searcher, ok := s.searchers[searchType]
	if !ok {
		return domain.SearchPage{}, ErrNotConfigured
	}
	text = strings.TrimSpace(text)
	key := fmt.Sprintf("%s\x00%d\x00%s", searchType, page, strings.ToLower(strings.Join(strings.Fields(text), " ")))

	s.mu.Lock()
	if e, ok := s.cache[key]; ok && s.now().Sub(e.at) <= searchFresh {
		s.mu.Unlock()
		return e.page, nil
	}
	f, joined := s.flights[key]
	if !joined {
		f = &searchFlight{done: make(chan struct{})}
		s.flights[key] = f
		// The call outlives any one caller: others may be waiting on it.
		callCtx := context.WithoutCancel(ctx)
		go s.run(callCtx, searcher, SearchQuery{Text: text, Page: page}, key, f)
	}
	s.mu.Unlock()

	select {
	case <-f.done:
		return f.page, f.err
	case <-ctx.Done():
		return domain.SearchPage{}, ctx.Err()
	}
}

func (s *SearchService) run(ctx context.Context, searcher Searcher, q SearchQuery, key string, f *searchFlight) {
	ctx, cancel := context.WithTimeout(ctx, searchBudget)
	defer cancel()
	page, err := searcher.Search(ctx, q)
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case err == nil:
		s.storeLocked(key, page)
	case !errors.Is(err, ErrCredentials) && !errors.Is(err, ErrNotConfigured):
		// Busy or down: an older answer beats an error. A rejected credential
		// is not an outage, and an old answer must not hide a broken setup.
		if e, ok := s.cache[key]; ok && s.now().Sub(e.at) <= searchStale {
			page, err = e.page, nil
		}
	}
	f.page, f.err = page, err
	delete(s.flights, key)
	close(f.done)
}

func (s *SearchService) storeLocked(key string, page domain.SearchPage) {
	if _, replacing := s.cache[key]; !replacing && len(s.cache) >= searchLimit {
		oldest, oldestAt := "", s.now()
		for k, e := range s.cache {
			if oldest == "" || e.at.Before(oldestAt) {
				oldest, oldestAt = k, e.at
			}
		}
		delete(s.cache, oldest)
	}
	s.cache[key] = searchEntry{page: page, at: s.now()}
}
