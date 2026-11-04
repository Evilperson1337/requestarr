package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/evilperson1337/requestarr/internal/auth"
	"github.com/evilperson1337/requestarr/internal/buildinfo"
	"github.com/evilperson1337/requestarr/internal/domain"
)

type fakeLocal struct {
	mu     sync.Mutex
	calls  int
	keys   [][]domain.SearchKey
	states map[domain.SearchKey]domain.SearchLocal
	err    error
}

func (f *fakeLocal) SearchLocalStates(_ context.Context, keys []domain.SearchKey) (map[domain.SearchKey]domain.SearchLocal, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.keys = append(f.keys, keys)
	return f.states, f.err
}

func searchHandlerWithLocal(t *testing.T, role string, svc SearchService, local SearchLocalService) http.Handler {
	t.Helper()
	d := testDeps(testLogger(), buildinfo.Info{}, fakeReadiness{}, nil, nil, nil,
		&auth.Handler{Repo: csrfAuthRepo{statusAuthRepo{user: auth.User{ID: matrixUserID, Role: role}}}, Secret: "test-secret", PublicURL: "http://example.test"})
	d.Search, d.SearchLocal = svc, local
	h, err := NewRouter(d)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestSearchResultsCarryLocalStateFromOneBatchedLookup(t *testing.T) {
	two := append(append([]domain.SearchResult{}, duneResults...), domain.SearchResult{MediaType: domain.MediaTV, Provider: "tmdb", ProviderID: "438631", Ref: "https://www.themoviedb.org/tv/438631", Title: "Dune: Prophecy"})
	f := &fakeSearcher{results: two}
	local := &fakeLocal{states: map[domain.SearchKey]domain.SearchLocal{
		{MediaType: domain.MediaMovie, Provider: "tmdb", ProviderID: "438631"}: {CatalogID: "c-1", RequestStatus: domain.RequestApproved, Availability: domain.AvailabilityUnknown},
	}}
	h := searchHandlerWithLocal(t, "viewer", movieTVService(f), local)
	w := searchGet(h, "/search?q=dune&type=movie_tv", true)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var page domain.SearchPage
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Results) != 2 {
		t.Fatalf("results must never be hidden: %s", w.Body.String())
	}
	if l := page.Results[0].Local; l == nil || l.CatalogID != "c-1" || l.RequestStatus != domain.RequestApproved {
		t.Fatalf("movie local = %+v", l)
	}
	if page.Results[1].Local != nil || strings.Contains(w.Body.String(), `"local":null`) {
		t.Fatalf("a never-resolved item has no local state at all: %s", w.Body.String())
	}
	if local.calls != 1 || len(local.keys[0]) != 2 || local.keys[0][1].MediaType != domain.MediaTV {
		t.Fatalf("lookups = %d keys = %+v", local.calls, local.keys)
	}
}

func TestSearchLocalStateIsFreshOnEveryRequestEvenWhenTheProviderAnswerIsCached(t *testing.T) {
	f := &fakeSearcher{results: duneResults}
	local := &fakeLocal{}
	h := searchHandlerWithLocal(t, "viewer", movieTVService(f), local)
	searchGet(h, "/search?q=dune&type=movie_tv", true)
	local.states = map[domain.SearchKey]domain.SearchLocal{{MediaType: domain.MediaMovie, Provider: "tmdb", ProviderID: "438631"}: {Availability: domain.AvailabilityAvailable}}
	w := searchGet(h, "/search?q=dune&type=movie_tv", true)
	if f.callCount() != 1 || !strings.Contains(w.Body.String(), `"availability":"available"`) {
		t.Fatalf("provider calls %d body %s", f.callCount(), w.Body.String())
	}
	local.states = nil
	if w := searchGet(h, "/search?q=dune&type=movie_tv", true); strings.Contains(w.Body.String(), `"local"`) {
		t.Fatalf("annotation leaked into the cached copy: %s", w.Body.String())
	}
}

func TestSearchStillAnswersWhenTheLocalLookupFails(t *testing.T) {
	h := searchHandlerWithLocal(t, "viewer", movieTVService(&fakeSearcher{results: duneResults}), &fakeLocal{err: errors.New("db down")})
	w := searchGet(h, "/search?q=dune&type=movie_tv", true)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Dune") || strings.Contains(w.Body.String(), `"local"`) {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}
