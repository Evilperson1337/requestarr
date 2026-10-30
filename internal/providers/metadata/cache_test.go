package metadata

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/domain"
)

type countingResolver struct {
	calls int
	err   error
}

func (*countingResolver) Name() string               { return "fixture" }
func (*countingResolver) CanResolve(u *url.URL) bool { return u.Hostname() == "www.igdb.com" }
func (c *countingResolver) Resolve(_ context.Context, u *url.URL) (domain.ResolvedItem, error) {
	c.calls++
	if c.err != nil {
		return domain.ResolvedItem{}, c.err
	}
	return domain.ResolvedItem{Item: domain.CatalogItem{Title: fmt.Sprintf("%s #%d", u.Path, c.calls)}}, nil
}

const link = "https://www.igdb.com/games/outer-wilds"

func cached(t *testing.T) (*Service, *countingResolver, *time.Time) {
	t.Helper()
	r := &countingResolver{}
	s := NewService(nil, r)
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	return s, r, &now
}

// Pasting the same link again does not ask the provider again.
func TestRepeatedLinksAreServedFromAShortCache(t *testing.T) {
	s, r, now := cached(t)
	first, _ := s.Resolve(context.Background(), link)
	*now = now.Add(9 * time.Minute)
	second, _ := s.Resolve(context.Background(), link)
	if r.calls != 1 || first.Item.Title != second.Item.Title {
		t.Fatalf("calls = %d, %q vs %q", r.calls, first.Item.Title, second.Item.Title)
	}
	*now = now.Add(2 * time.Minute) // past the freshness window
	third, _ := s.Resolve(context.Background(), link)
	if r.calls != 2 || third.Item.Title == first.Item.Title {
		t.Fatalf("a stale entry must be refreshed: calls = %d", r.calls)
	}
}

// When the provider is busy, the last good answer is better than an error.
func TestAStaleAnswerCoversABusyProviderButNothingElse(t *testing.T) {
	s, r, now := cached(t)
	good, _ := s.Resolve(context.Background(), link)
	*now = now.Add(time.Hour)
	r.err = fmt.Errorf("provider returned 503: %w", ErrBusy)
	got, err := s.Resolve(context.Background(), link)
	if err != nil || got.Item.Title != good.Item.Title {
		t.Fatalf("busy provider: err=%v title=%q", err, got.Item.Title)
	}
	r.err = errors.New("IGDB game not found")
	if _, err := s.Resolve(context.Background(), link); err == nil {
		t.Fatal("a real failure must not be hidden by a stale answer")
	}
	*now = now.Add(48 * time.Hour) // beyond the stale window
	r.err = fmt.Errorf("busy: %w", ErrBusy)
	if _, err := s.Resolve(context.Background(), link); !errors.Is(err, ErrBusy) {
		t.Fatalf("a very old answer must not be served: %v", err)
	}
}

func TestFailuresAreNeverCachedAndTheCacheIsBounded(t *testing.T) {
	s, r, _ := cached(t)
	r.err = errors.New("nope")
	for i := 0; i < 2; i++ {
		_, _ = s.Resolve(context.Background(), link)
	}
	if r.calls != 2 {
		t.Fatalf("an error was cached: calls = %d", r.calls)
	}
	r.err = nil
	for i := 0; i < cacheLimit+50; i++ {
		_, _ = s.Resolve(context.Background(), fmt.Sprintf("https://www.igdb.com/games/game-%d", i))
	}
	if len(s.cache) > cacheLimit {
		t.Fatalf("cache holds %d entries, limit %d", len(s.cache), cacheLimit)
	}
}
