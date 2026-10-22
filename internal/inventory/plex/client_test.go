package plex

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/inventory"
	"github.com/evilperson1337/requestarr/internal/inventory/inventorytest"
)

const testToken = "secret-plex-token"

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// backend serves the recorded fixtures, bending them per scenario. It records
// every request so tests can assert what the adapter asked for.
type backend struct {
	t        *testing.T
	scenario inventorytest.Scenario
	mu       sync.Mutex
	requests []*http.Request
}

func (b *backend) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	b.requests = append(b.requests, r.Clone(context.Background()))
	b.mu.Unlock()
	if r.Header.Get("X-Plex-Token") != testToken || b.scenario == inventorytest.Unauthorized {
		http.Error(w, "unauthorized "+testToken, http.StatusUnauthorized) // a hostile body that echoes the token
		return
	}
	if b.scenario == inventorytest.ServerError {
		http.Error(w, "boom", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	section, offset := r.URL.Path, r.URL.Query().Get("X-Plex-Container-Start")
	switch {
	case section == "/library/sections":
		_, _ = w.Write(fixture(b.t, "sections.json"))
	case b.scenario == inventorytest.Empty:
		_, _ = w.Write(fixture(b.t, "empty_section.json"))
	case section == "/library/sections/1/all" && b.scenario == inventorytest.Malformed:
		_, _ = w.Write(fixture(b.t, "malformed.json"))
	case section == "/library/sections/1/all" && b.scenario == inventorytest.Truncated:
		_, _ = w.Write(fixture(b.t, "truncated.json"))
	case section == "/library/sections/1/all" && offset == "0":
		_, _ = w.Write(fixture(b.t, "movies_page1.json"))
	case section == "/library/sections/1/all" && offset == "2":
		_, _ = w.Write(fixture(b.t, "movies_page2.json"))
	case section == "/library/sections/2/all" && b.scenario == inventorytest.MidwayFailure:
		http.Error(w, "boom", http.StatusInternalServerError)
	case section == "/library/sections/2/all" && offset == "0":
		_, _ = w.Write(fixture(b.t, "shows_page1.json"))
	default:
		http.NotFound(w, r)
	}
}

func newBackendClient(t *testing.T, scenario inventorytest.Scenario) (*Client, *backend) {
	t.Helper()
	b := &backend{t: t, scenario: scenario}
	ts := httptest.NewServer(b)
	t.Cleanup(ts.Close)
	c, err := NewWithHTTPClient(ts.URL, testToken, ts.Client())
	if err != nil {
		t.Fatal(err)
	}
	c.pageSize = 2 // the fixtures are recorded at page size 2
	return c, b
}

func TestPlexPassesTheInventorySourceContract(t *testing.T) {
	inventorytest.Run(t, func(t *testing.T, s inventorytest.Scenario) inventory.Source {
		c, _ := newBackendClient(t, s)
		return c
	}, inventorytest.Expectations{LibraryItems: 5, Secret: testToken})
}

func TestSnapshotNormalizesMoviesAndShowsAndReadsOnlyThoseLibraries(t *testing.T) {
	c, b := newBackendClient(t, inventorytest.Library)
	snap, err := c.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]inventory.Item{}
	for _, it := range snap.Items {
		byID[it.SourceItemID] = it
	}
	matrix := byID["101"]
	if matrix.MediaType != domain.MediaMovie || matrix.Title != "The Matrix" || matrix.Year != 1999 || matrix.ExternalIDs["tmdb"] != "603" || matrix.ExternalIDs["imdb"] != "tt0133093" || matrix.ExternalIDs["tvdb"] != "169" {
		t.Fatalf("matrix=%+v", matrix)
	}
	if home := byID["102"]; home.Title != "Home Movie" || len(home.ExternalIDs) != 0 {
		t.Fatalf("an item without ids must still be inventory, just unmatchable: %+v", home)
	}
	if sev := byID["201"]; sev.MediaType != domain.MediaTV || sev.ExternalIDs["tmdb"] != "95396" {
		t.Fatalf("severance=%+v", sev)
	}
	for _, r := range b.requests {
		if strings.HasPrefix(r.URL.Path, "/library/sections/3") {
			t.Fatalf("the music library was read: %s", r.URL)
		}
	}
}

func TestTokenTravelsOnlyInTheHeader(t *testing.T) {
	c, b := newBackendClient(t, inventorytest.Library)
	if _, err := c.Snapshot(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(b.requests) < 3 {
		t.Fatalf("only %d requests", len(b.requests))
	}
	for _, r := range b.requests {
		if strings.Contains(r.URL.String(), testToken) {
			t.Fatalf("token in URL: %s", r.URL)
		}
		if r.Header.Get("Accept") != "application/json" {
			t.Errorf("Accept=%q", r.Header.Get("Accept"))
		}
	}
}

func TestServerWithoutMovieOrTVLibrariesIsAnErrorNotAnEmptyInventory(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture(t, "only_music.json"))
	}))
	defer ts.Close()
	c, err := NewWithHTTPClient(ts.URL, testToken, ts.Client())
	if err != nil {
		t.Fatal(err)
	}
	if snap, err := c.Snapshot(context.Background()); err == nil || snap.Complete {
		t.Fatalf("err=%v complete=%v: a token that sees no movie/TV library must not read as an empty library", err, snap.Complete)
	}
}

func TestNewRejectsUnsafeConfiguration(t *testing.T) {
	for _, tc := range []struct{ url, token string }{
		{"", "t"}, {"ftp://plex:32400", "t"}, {"http://user:pw@plex:32400", "t"}, {"http://plex:32400/path", "t"}, {"http://plex:32400?x=1", "t"}, {"http://plex:32400", ""},
	} {
		if _, err := New(tc.url, tc.token); err == nil {
			t.Errorf("New(%q,%q) accepted", tc.url, tc.token)
		}
	}
	if _, err := New("https://plex.example:32400", "t"); err != nil {
		t.Fatal(err)
	}
}
