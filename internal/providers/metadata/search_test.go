package metadata

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/domain"
)

type blockingSearcher struct {
	started   chan struct{}
	cancelled chan struct{}
	calls     atomic.Int32
	last      atomic.Value // SearchQuery
}

func newBlockingSearcher() *blockingSearcher {
	return &blockingSearcher{started: make(chan struct{}, 8), cancelled: make(chan struct{}, 8)}
}

func (b *blockingSearcher) Search(ctx context.Context, q SearchQuery) (domain.SearchPage, error) {
	b.calls.Add(1)
	b.last.Store(q)
	b.started <- struct{}{}
	<-ctx.Done()
	b.cancelled <- struct{}{}
	return domain.SearchPage{}, ctx.Err()
}

func expect(t *testing.T, ch chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

// A person who gives up (the browser cancels) must not leave the provider
// call running: for a rate-limited provider it holds a slot someone else needs.
func TestProviderCallStopsWhenEveryCallerHasGivenUp(t *testing.T) {
	b := newBlockingSearcher()
	svc := NewSearchService(map[string]Searcher{"album": b})
	ctx1, cancel1 := context.WithCancel(context.Background())
	ctx2, cancel2 := context.WithCancel(context.Background())
	for _, ctx := range []context.Context{ctx1, ctx2} {
		go svc.Search(ctx, "album", "dark side", 1, SearchOptions{})
		if ctx == ctx1 {
			expect(t, b.started, "the provider call to start")
		}
	}
	time.Sleep(20 * time.Millisecond) // the second caller joins the same call
	cancel1()
	select {
	case <-b.cancelled:
		t.Fatal("the call was cancelled while a caller still waited for it")
	case <-time.After(50 * time.Millisecond):
	}
	cancel2()
	expect(t, b.cancelled, "the call to be cancelled")
	if n := b.calls.Load(); n != 1 {
		t.Fatalf("provider calls = %d, want 1 shared", n)
	}
}

func TestSearchOptionsReachTheProviderAndSeparateTheCache(t *testing.T) {
	var seen []SearchQuery
	svc := NewSearchService(map[string]Searcher{"album": searcherFunc(func(_ context.Context, q SearchQuery) (domain.SearchPage, error) {
		seen = append(seen, q)
		return domain.SearchPage{Results: []domain.SearchResult{{Title: "x"}}}, nil
	})})
	for _, singles := range []bool{false, true, false} {
		if _, err := svc.Search(context.Background(), "album", "dark", 1, SearchOptions{IncludeSingles: singles}); err != nil {
			t.Fatal(err)
		}
	}
	if len(seen) != 2 || seen[0].IncludeSingles || !seen[1].IncludeSingles {
		t.Fatalf("calls = %+v; the toggle must reach the provider and not share a cache entry", seen)
	}
}

type searcherFunc func(context.Context, SearchQuery) (domain.SearchPage, error)

func (f searcherFunc) Search(ctx context.Context, q SearchQuery) (domain.SearchPage, error) {
	return f(ctx, q)
}
