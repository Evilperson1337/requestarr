package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/evilperson1337/requestarr/internal/auth"
	"github.com/evilperson1337/requestarr/internal/buildinfo"
	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/providers/metadata"
)

// fakeSearcher stands in for TMDB behind the real search service.
type fakeSearcher struct {
	mu      sync.Mutex
	calls   int
	texts   []string
	err     error
	block   chan struct{}
	results []domain.SearchResult
}

func (f *fakeSearcher) Search(_ context.Context, q metadata.SearchQuery) (domain.SearchPage, error) {
	f.mu.Lock()
	f.calls++
	f.texts = append(f.texts, q.Text)
	block, err := f.block, f.err
	f.mu.Unlock()
	if block != nil {
		<-block
	}
	if err != nil {
		return domain.SearchPage{}, err
	}
	return domain.SearchPage{Results: f.results, HasMore: true}, nil
}

func (f *fakeSearcher) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakeSearcher) setErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

var duneResults = []domain.SearchResult{{MediaType: domain.MediaMovie, Provider: "tmdb", ProviderID: "438631", Ref: "https://www.themoviedb.org/movie/438631", Title: "Dune", Year: 2021, ArtworkURL: "https://image.tmdb.org/t/p/w500/dune.jpg", ArtworkKind: domain.ArtworkPoster}}

func searchHandler(t *testing.T, role string, svc SearchService) http.Handler {
	t.Helper()
	d := testDeps(testLogger(), buildinfo.Info{}, fakeReadiness{}, nil, nil, nil,
		&auth.Handler{Repo: csrfAuthRepo{statusAuthRepo{user: auth.User{ID: matrixUserID, Role: role}}}, Secret: "test-secret", PublicURL: "http://example.test"})
	d.Search = svc
	h, err := NewRouter(d)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func searchGet(h http.Handler, path string, authed bool) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, "/api/v1"+path, nil)
	if authed {
		r.AddCookie(&http.Cookie{Name: "requestarr_session", Value: "valid"})
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func movieTVService(f *fakeSearcher) SearchService {
	return metadata.NewSearchService(map[string]metadata.Searcher{"movie_tv": f})
}

func TestSearchReturnsResultsForEveryRoleWithoutCaching(t *testing.T) {
	for _, role := range []string{"viewer", "requester", "admin"} {
		f := &fakeSearcher{results: duneResults}
		w := searchGet(searchHandler(t, role, movieTVService(f)), "/search?q=dune&type=movie_tv", true)
		if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s: %d %v %s", role, w.Code, w.Header(), w.Body.String())
		}
		var page struct {
			Results []struct {
				Title string `json:"title"`
				Ref   string `json:"ref"`
				Year  int    `json:"year"`
			} `json:"results"`
			HasMore bool `json:"hasMore"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		if len(page.Results) != 1 || page.Results[0].Title != "Dune" || page.Results[0].Year != 2021 || !page.HasMore {
			t.Fatalf("%s: %s", role, w.Body.String())
		}
	}
}

func TestSearchRequiresASession(t *testing.T) {
	f := &fakeSearcher{results: duneResults}
	if w := searchGet(searchHandler(t, "viewer", movieTVService(f)), "/search?q=dune&type=movie_tv", false); w.Code != 401 || f.callCount() != 0 {
		t.Fatalf("%d calls=%d", w.Code, f.callCount())
	}
}

func TestSearchRejectsInvalidInputWithoutCallingTheProvider(t *testing.T) {
	f := &fakeSearcher{results: duneResults}
	h := searchHandler(t, "requester", movieTVService(f))
	for name, query := range map[string]string{
		"too short":       "q=d&type=movie_tv",
		"short once trim": "q=%20d%20&type=movie_tv",
		"too long":        "q=" + strings.Repeat("a", 201) + "&type=movie_tv",
		"control char":    "q=du%00ne&type=movie_tv",
		"newline":         "q=du%0Ane&type=movie_tv",
		"missing type":    "q=dune",
		"unknown type":    "q=dune&type=podcast",
		"bad page":        "q=dune&type=movie_tv&page=0",
		"page too high":   "q=dune&type=movie_tv&page=11",
		"page not number": "q=dune&type=movie_tv&page=x",
	} {
		w := searchGet(h, "/search?"+query, true)
		if w.Code != 400 || !strings.Contains(w.Body.String(), `"invalid_search"`) {
			t.Errorf("%s: %d %s", name, w.Code, w.Body.String())
		}
	}
	if f.callCount() != 0 {
		t.Fatalf("provider called %d times for invalid input", f.callCount())
	}
	// The limits are inclusive.
	if w := searchGet(h, "/search?q=%20du%20&type=movie_tv", true); w.Code != 200 || f.texts[0] != "du" {
		t.Fatalf("two characters after trim: %d %v", w.Code, f.texts)
	}
	if w := searchGet(h, "/search?q="+url.QueryEscape(strings.Repeat("é", 200))+"&type=movie_tv", true); w.Code != 200 {
		t.Fatalf("200 characters: %d", w.Code)
	}
}
