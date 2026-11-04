package httpapi

import (
	"context"
	"errors"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/auth"
	"github.com/evilperson1337/requestarr/internal/buildinfo"
	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/providers/metadata"
)

func TestSearchProviderFailureIsAnErrorNeverAnEmptyList(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"busy", metadata.ErrBusy, 503, "metadata_provider_busy"},
		{"credentials", metadata.ErrCredentials, 502, "metadata_provider_credentials"},
		{"not configured", metadata.ErrNotConfigured, 503, "metadata_provider_not_configured"},
		{"unreadable answer", errors.New("decode: invalid character"), 503, "metadata_provider_busy"},
	} {
		f := &fakeSearcher{err: tc.err}
		w := searchGet(searchHandler(t, "requester", movieTVService(f)), "/search?q=dune&type=movie_tv", true)
		if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.code) || strings.Contains(w.Body.String(), `"results"`) {
			t.Errorf("%s: %d %s", tc.name, w.Code, w.Body.String())
		}
	}
	// A type with no configured provider is not-configured, not empty.
	w := searchGet(searchHandler(t, "requester", metadata.NewSearchService(nil)), "/search?q=dune&type=movie_tv", true)
	if w.Code != 503 || !strings.Contains(w.Body.String(), "metadata_provider_not_configured") {
		t.Fatalf("unconfigured: %d %s", w.Code, w.Body.String())
	}
}

func TestSearchTypesListsOnlyConfiguredProviders(t *testing.T) {
	w := searchGet(searchHandler(t, "viewer", movieTVService(&fakeSearcher{})), "/search/types", true)
	if w.Code != 200 || strings.TrimSpace(w.Body.String()) != `{"types":["movie_tv"]}` {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	w = searchGet(searchHandler(t, "viewer", metadata.NewSearchService(nil)), "/search/types", true)
	if w.Code != 200 || strings.TrimSpace(w.Body.String()) != `{"types":[]}` {
		t.Fatalf("none configured: %d %s", w.Code, w.Body.String())
	}
	if w := searchGet(searchHandler(t, "viewer", movieTVService(&fakeSearcher{})), "/search/types", false); w.Code != 401 {
		t.Fatalf("anonymous: %d", w.Code)
	}
}

func TestSearchRepeatWithinFreshWindowCostsNoProviderCall(t *testing.T) {
	f := &fakeSearcher{results: duneResults}
	h := searchHandler(t, "requester", movieTVService(f))
	for _, q := range []string{"dune", "  Dune ", "DUNE"} {
		if w := searchGet(h, "/search?type=movie_tv&q="+url.QueryEscape(q), true); w.Code != 200 {
			t.Fatal(w.Code)
		}
	}
	if f.callCount() != 1 {
		t.Fatalf("provider calls = %d, want 1", f.callCount())
	}
	// A different page is a different query.
	searchGet(h, "/search?type=movie_tv&q=dune&page=2", true)
	if f.callCount() != 2 {
		t.Fatalf("provider calls = %d, want 2", f.callCount())
	}
}

func TestSearchStaleCopyServesOnlyWhenProviderIsBusyOrDown(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	f := &fakeSearcher{results: duneResults}
	svc := metadata.NewSearchService(map[string]metadata.Searcher{"movie_tv": f})
	svc.SetClock(func() time.Time { return now })
	h := searchHandler(t, "requester", svc)
	get := func() *httptest.ResponseRecorder { return searchGet(h, "/search?type=movie_tv&q=dune", true) }
	get()
	now = now.Add(5 * time.Minute) // past fresh, within stale
	f.setErr(metadata.ErrBusy)
	if w := get(); w.Code != 200 || !strings.Contains(w.Body.String(), "Dune") {
		t.Fatalf("busy should fall back to stale: %d %s", w.Code, w.Body.String())
	}
	f.setErr(errors.New("connection refused"))
	if w := get(); w.Code != 200 {
		t.Fatalf("down should fall back to stale: %d", w.Code)
	}
	// Rejected credentials are not "busy": no stale answer hides a broken setup.
	f.setErr(metadata.ErrCredentials)
	if w := get(); w.Code != 502 {
		t.Fatalf("credentials must surface: %d", w.Code)
	}
	// A healthy provider is asked again rather than answered from the stale copy.
	f.setErr(nil)
	before := f.callCount()
	if w := get(); w.Code != 200 || f.callCount() != before+1 {
		t.Fatalf("healthy provider must be asked again: %d", w.Code)
	}
	now = now.Add(7 * time.Hour) // past stale
	f.setErr(metadata.ErrBusy)
	if w := get(); w.Code != 503 {
		t.Fatalf("too old to serve: %d", w.Code)
	}
}

func TestSearchIdenticalConcurrentQueriesMakeOneProviderCall(t *testing.T) {
	f := &fakeSearcher{results: duneResults, block: make(chan struct{})}
	h := searchHandler(t, "requester", movieTVService(f))
	var wg sync.WaitGroup
	codes := make([]int, 5)
	for i := range codes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i] = searchGet(h, "/search?type=movie_tv&q=dune", true).Code
		}()
	}
	deadline := time.Now().Add(2 * time.Second)
	for f.callCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond) // let the other callers join the flight
	close(f.block)
	wg.Wait()
	for _, c := range codes {
		if c != 200 {
			t.Fatalf("codes = %v", codes)
		}
	}
	if f.callCount() != 1 {
		t.Fatalf("provider calls = %d, want 1", f.callCount())
	}
}

// A search neither resolves nor stores anything: the resolver, which is the
// only path into the catalog, is never touched.
func TestSearchNeverTouchesTheResolverOrCatalog(t *testing.T) {
	d := testDeps(testLogger(), buildinfo.Info{}, fakeReadiness{}, upsertForbidden{t}, nil, nil,
		&auth.Handler{Repo: csrfAuthRepo{statusAuthRepo{user: auth.User{ID: matrixUserID, Role: "requester"}}}, Secret: "test-secret", PublicURL: "http://example.test"})
	d.Search = movieTVService(&fakeSearcher{results: duneResults})
	h, err := NewRouter(d)
	if err != nil {
		t.Fatal(err)
	}
	if w := searchGet(h, "/search?type=movie_tv&q=dune", true); w.Code != 200 {
		t.Fatal(w.Code)
	}
}

type upsertForbidden struct{ t *testing.T }

func (u upsertForbidden) ResolveAndUpsert(context.Context, string) (domain.ResolvedItem, error) {
	u.t.Error("a search resolved or stored an item")
	return domain.ResolvedItem{}, errors.New("forbidden")
}
