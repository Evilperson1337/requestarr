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
	case section == "/library/sections/3/all" && r.URL.Query().Get("type") == "9" && offset == "0":
		_, _ = w.Write(fixture(b.t, "albums_page1.json"))
	case section == "/library/sections/3/all" && r.URL.Query().Get("type") == "10" && offset == "0":
		_, _ = w.Write(fixture(b.t, "tracks_page1.json"))
	case section == "/library/sections/3/all" && r.URL.Query().Get("type") == "10" && offset == "2":
		_, _ = w.Write(fixture(b.t, "tracks_page2.json"))
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
	}, inventorytest.Expectations{LibraryItems: 10, Secret: testToken})
}

func TestSnapshotNormalizesMoviesShowsAlbumsAndTracks(t *testing.T) {
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
	ok := byID["301"]
	if ok.MediaType != domain.MediaAlbum || ok.Title != "OK Computer" || ok.Year != 1997 || ok.ExternalIDs["mbid"] != "3a1f0e5e-1c1a-4e7a-9d6b-0b9a8f2f6a01" {
		t.Fatalf("album=%+v", ok)
	}
	if untagged := byID["302"]; untagged.MediaType != domain.MediaAlbum || len(untagged.ExternalIDs) != 0 {
		t.Fatalf("an album Plex never matched must be inventory without ids: %+v", untagged)
	}
	if paranoid := byID["401"]; paranoid.MediaType != domain.MediaSong || paranoid.ExternalIDs["mbid"] != "7b2e1f6a-2d2b-4f8b-8e7c-1c0b9a3f7b02" {
		t.Fatalf("track=%+v", paranoid)
	}
	// Older agents put the MusicBrainz id only in the top-level guid string.
	if karma := byID["402"]; karma.ExternalIDs["mbid"] != "8c3f2a7b-3e3c-4a9c-9f8d-2d1c0b4a8c03" {
		t.Fatalf("legacy guid track=%+v", karma)
	}
	if home := byID["403"]; home.MediaType != domain.MediaSong || len(home.ExternalIDs) != 0 {
		t.Fatalf("track=%+v", home)
	}
	asked := map[string]bool{}
	for _, r := range b.requests {
		if strings.HasPrefix(r.URL.Path, "/library/sections/3/all") {
			asked[r.URL.Query().Get("type")] = true
		}
	}
	if !asked["9"] || !asked["10"] {
		t.Fatalf("music library must be read as albums (type 9) and tracks (type 10): %v", asked)
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

func TestServerWithoutAnyReadableLibraryIsAnErrorNotAnEmptyInventory(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture(t, "only_photos.json"))
	}))
	defer ts.Close()
	c, err := NewWithHTTPClient(ts.URL, testToken, ts.Client())
	if err != nil {
		t.Fatal(err)
	}
	if snap, err := c.Snapshot(context.Background()); err == nil || snap.Complete {
		t.Fatalf("err=%v complete=%v: a token that sees no movie, TV or music library must not read as an empty library", err, snap.Complete)
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

func TestMusicTracksFailingAfterAlbumsSucceededDiscardsEverything(t *testing.T) {
	b := &backend{t: t, scenario: inventorytest.Library}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Movies, shows and albums all succeed; the tracks' second page fails.
		if r.URL.Path == "/library/sections/3/all" && r.URL.Query().Get("type") == "10" && r.URL.Query().Get("X-Plex-Container-Start") == "2" {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		b.ServeHTTP(w, r)
	}))
	defer ts.Close()
	c, err := NewWithHTTPClient(ts.URL, testToken, ts.Client())
	if err != nil {
		t.Fatal(err)
	}
	c.pageSize = 2
	snap, err := c.Snapshot(context.Background())
	if err == nil || snap.Complete || len(snap.Items) != 0 {
		t.Fatalf("err=%v complete=%v items=%d: movies, shows and albums were read, but a failed tracks page must discard the lot", err, snap.Complete, len(snap.Items))
	}
}

// A real library had one album with no title, and the strict check failed the
// whole sync. Such an item is still inventory (it may match by id), and an
// overlong title is shortened, never a reason to discard a complete listing.
func TestUntitledAndOverlongTitlesDoNotFailTheSync(t *testing.T) {
	long := strings.Repeat("é", 600)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/library/sections" {
			_, _ = w.Write([]byte(`{"MediaContainer":{"Directory":[{"key":"1","type":"movie"}]}}`))
			return
		}
		_, _ = w.Write([]byte(`{"MediaContainer":{"totalSize":3,"offset":0,"Metadata":[
			{"ratingKey":"1","title":"","year":2001,"Guid":[{"id":"tmdb://5"}]},
			{"ratingKey":"2","title":"` + long + `"},
			{"ratingKey":"3","title":"Normal"}]}}`))
	}))
	defer server.Close()
	c, err := New(server.URL, testToken)
	if err != nil {
		t.Fatal(err)
	}
	snap, err := c.Snapshot(context.Background())
	if err != nil || len(snap.Items) != 3 || !snap.Complete {
		t.Fatalf("snapshot err=%v items=%d", err, len(snap.Items))
	}
	if snap.Items[0].Title != "(untitled)" || snap.Items[0].ExternalIDs["tmdb"] != "5" {
		t.Fatalf("untitled item = %+v", snap.Items[0])
	}
	if n := len([]rune(snap.Items[1].Title)); n != 512 {
		t.Fatalf("overlong title has %d runes", n)
	}
}
