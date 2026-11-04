package audiobookshelf

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

const testKey = "secret-abs-api-key"

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// backend serves fixtures shaped like Audiobookshelf's minified item lists,
// bending them per scenario.
type backend struct {
	t         *testing.T
	scenario  inventorytest.Scenario
	libraries string // overrides the library list for ad-hoc tests
	mu        sync.Mutex
	requests  []*http.Request
}

func (b *backend) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	b.requests = append(b.requests, r.Clone(context.Background()))
	b.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer "+testKey || b.scenario == inventorytest.Unauthorized {
		http.Error(w, "unauthorized "+testKey, http.StatusUnauthorized) // a hostile body that echoes the key
		return
	}
	if b.scenario == inventorytest.ServerError {
		http.Error(w, "boom", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	page := r.URL.Query().Get("page")
	switch {
	case r.URL.Path == "/api/libraries" && b.libraries != "":
		_, _ = w.Write([]byte(b.libraries))
	case r.URL.Path == "/api/libraries":
		_, _ = w.Write(fixture(b.t, "libraries.json"))
	case r.URL.Path != "/api/libraries/lib_books/items":
		http.NotFound(w, r)
	case b.scenario == inventorytest.Empty:
		_, _ = w.Write(fixture(b.t, "items_empty.json"))
	case b.scenario == inventorytest.Malformed:
		_, _ = w.Write(fixture(b.t, "items_malformed.json"))
	case b.scenario == inventorytest.Truncated:
		_, _ = w.Write(fixture(b.t, "items_truncated.json"))
	case page == "0":
		_, _ = w.Write(fixture(b.t, "items_page1.json"))
	case page == "1" && b.scenario == inventorytest.MidwayFailure:
		http.Error(w, "boom", http.StatusInternalServerError)
	case page == "1":
		_, _ = w.Write(fixture(b.t, "items_page2.json"))
	case page == "2":
		_, _ = w.Write(fixture(b.t, "items_page3.json"))
	default:
		http.NotFound(w, r)
	}
}

func newBackendClient(t *testing.T, b *backend) *Client {
	t.Helper()
	b.t = t
	ts := httptest.NewServer(b)
	t.Cleanup(ts.Close)
	c, err := NewWithHTTPClient(ts.URL, testKey, ts.Client())
	if err != nil {
		t.Fatal(err)
	}
	c.pageSize = 2 // the fixtures are laid out at page size 2
	return c
}

func TestAudiobookshelfPassesTheInventorySourceContract(t *testing.T) {
	inventorytest.Run(t, func(t *testing.T, s inventorytest.Scenario) inventory.Source {
		return newBackendClient(t, &backend{scenario: s})
	}, inventorytest.Expectations{LibraryItems: 3, Secret: testKey})
}

func TestDescriptor(t *testing.T) {
	d := newBackendClient(t, &backend{}).Descriptor()
	if len(d.MediaTypes) != 1 || d.MediaTypes[0] != domain.MediaAudiobook || len(d.IdentifierKinds) != 1 || d.IdentifierKinds[0] != "audible" || d.Kind != "audiobookshelf" {
		t.Fatalf("descriptor=%+v", d)
	}
}

func TestSnapshotKeepsAudioItemsAndMapsASINs(t *testing.T) {
	snap, err := newBackendClient(t, &backend{}).Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]inventory.Item{}
	for _, it := range snap.Items {
		byID[it.SourceItemID] = it
	}
	if len(byID) != 3 {
		t.Fatalf("items=%v", byID)
	}
	hail := byID["li_1"]
	if hail.MediaType != domain.MediaAudiobook || hail.Title != "Project Hail Mary" || hail.ExternalIDs["audible"] != "B08G9PRS1K" || hail.Year != 0 || hail.ArtworkRef != "/api/items/li_1/cover" ||
		hail.Detail != "Andy Weir · read by Ray Porter" || hail.AddedAt.IsZero() || !strings.HasSuffix(hail.ExternalURL, "/item/li_1") {
		t.Fatalf("hail=%+v", hail)
	}
	// No ASIN, or an unusable one, still lists the item, with no identifier.
	if by := byID["li_2"]; len(by.ExternalIDs) != 0 || by.Detail != "Someone" || by.ArtworkRef != "" {
		t.Fatalf("hand-matched=%+v", by)
	}
	if bad := byID["li_5"]; len(bad.ExternalIDs) != 0 {
		t.Fatalf("bad asin=%+v", bad)
	}
	if _, present := byID["li_3"]; present {
		t.Fatal("an item whose files are missing counted as inventory")
	}
	if _, present := byID["li_4"]; present {
		t.Fatal("an ebook-only item counted as an audiobook")
	}
}

func TestOnlyBookLibrariesAreRead(t *testing.T) {
	b := &backend{}
	if _, err := newBackendClient(t, b).Snapshot(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, r := range b.requests {
		if strings.Contains(r.URL.Path, "lib_pods") {
			t.Fatalf("read the podcast library: %s", r.URL)
		}
	}
}

func TestNoBookLibraryIsAnErrorNotAnEmptyLibrary(t *testing.T) {
	for _, body := range []string{`{"libraries":[]}`, `{"libraries":[{"id":"p","mediaType":"podcast"}]}`, `{}`} {
		snap, err := newBackendClient(t, &backend{libraries: body}).Snapshot(context.Background())
		if err == nil || snap.Complete || len(snap.Items) != 0 {
			t.Errorf("%s: snapshot %+v, err %v", body, snap, err)
		}
	}
}

func TestKeyTravelsOnlyInTheAuthorizationHeaderAndEveryRequestIsAGET(t *testing.T) {
	b := &backend{}
	if _, err := newBackendClient(t, b).Snapshot(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(b.requests) < 4 {
		t.Fatalf("only %d requests", len(b.requests))
	}
	for _, r := range b.requests {
		if strings.Contains(r.URL.String(), testKey) {
			t.Errorf("key in URL %s", r.URL)
		}
		if r.Method != http.MethodGet {
			t.Errorf("%s %s: the adapter must only read", r.Method, r.URL)
		}
	}
}

func TestPagesAreAskedForMinifiedAndTheListMustAgreeWithItself(t *testing.T) {
	b := &backend{}
	if _, err := newBackendClient(t, b).Snapshot(context.Background()); err != nil {
		t.Fatal(err)
	}
	if q := b.requests[1].URL.Query(); q.Get("minified") != "1" || q.Get("limit") != "2" || q.Get("page") != "0" {
		t.Fatalf("first item request %s", b.requests[1].URL)
	}
	for name, body := range map[string]string{
		"duplicate id":    `{"results":[{"id":"x","mediaType":"book","media":{"metadata":{"title":"A"},"numTracks":1}},{"id":"x","mediaType":"book","media":{"metadata":{"title":"B"},"numTracks":1}}],"total":2}`,
		"missing total":   `{"results":[]}`,
		"more than total": `{"results":[{"id":"x","mediaType":"book","media":{"metadata":{"title":"A"},"numTracks":1}}],"total":0}`,
		"empty title":     `{"results":[{"id":"x","mediaType":"book","media":{"metadata":{"title":" "},"numTracks":1}}],"total":1}`,
		"not a book":      `{"results":[{"id":"x","mediaType":"podcast","media":{"metadata":{"title":"A"},"numTracks":1}}],"total":1}`,
		"null results":    `{"results":null,"total":1}`,
	} {
		bk := &backend{scenario: ""}
		bk.t = t
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.URL.Path == "/api/libraries" {
				_, _ = w.Write(fixture(t, "libraries.json"))
				return
			}
			_, _ = w.Write([]byte(body))
		}))
		c, _ := NewWithHTTPClient(ts.URL, testKey, ts.Client())
		c.pageSize = 2
		snap, err := c.Snapshot(context.Background())
		ts.Close()
		if err == nil || snap.Complete || len(snap.Items) != 0 {
			t.Errorf("%s: snapshot %+v, err %v", name, snap, err)
		}
	}
}

func TestNewRefusesAnythingButAnOrigin(t *testing.T) {
	for _, u := range []string{"", "ftp://abs.example", "https://u:p@abs.example", "https://abs.example/sub", "https://abs.example?x=1", "https:///"} {
		if _, err := New(u, "k"); err == nil {
			t.Errorf("New(%q) accepted", u)
		}
	}
	if _, err := New("https://abs.example", ""); err == nil {
		t.Error("empty key accepted")
	}
	if _, err := New("https://abs.example/", "k"); err != nil {
		t.Errorf("trailing slash refused: %v", err)
	}
}

func TestArtworkFetchesOnlyAnItemCoverWithTheKeyInAHeader(t *testing.T) {
	var got *http.Request
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Clone(context.Background())
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("\xff\xd8jpeg"))
	}))
	defer ts.Close()
	c, _ := NewWithHTTPClient(ts.URL, testKey, ts.Client())
	art, err := c.Artwork(context.Background(), "/api/items/li_1/cover")
	if err != nil || art.ContentType != "image/jpeg" || len(art.Data) == 0 {
		t.Fatalf("artwork %+v, err %v", art, err)
	}
	if got.Header.Get("Authorization") != "Bearer "+testKey || strings.Contains(got.URL.String(), testKey) || got.Method != http.MethodGet {
		t.Fatalf("request %s %s", got.Method, got.URL)
	}
	for _, ref := range []string{"", "/api/items/../libraries/cover", "/api/items/a/b/cover", "/api/libraries", "//evil.example/api/items/x/cover", "/api/items/x/cover?token=1"} {
		if _, err := c.Artwork(context.Background(), ref); err == nil {
			t.Errorf("ref %q accepted", ref)
		}
	}
}

func TestAudiobooksCarryTheirDescriptionGenresAndRuntime(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/libraries":
			_, _ = w.Write(fixture(t, "libraries.json"))
		default:
			_, _ = w.Write([]byte(`{"total":2,"results":[
				{"id":"li_a","addedAt":1700000000000,"mediaType":"book","media":{"numTracks":3,"duration":36030.4,"metadata":{"title":"Plain","descriptionPlain":"Plain words.","description":"<p>Ignored</p>","genres":["Sci-Fi","Humor"]}}},
				{"id":"li_b","addedAt":1700000000000,"mediaType":"book","media":{"numTracks":3,"metadata":{"title":"Html","description":"<p>First &amp; best.</p><p>Second<br>line</p><script>x</script>"}}}]}`))
		}
	}))
	defer ts.Close()
	c, err := NewWithHTTPClient(ts.URL, testKey, ts.Client())
	if err != nil {
		t.Fatal(err)
	}
	snap, err := c.Snapshot(context.Background())
	if err != nil || len(snap.Items) != 2 {
		t.Fatalf("snapshot %v %d", err, len(snap.Items))
	}
	if d := snap.Items[0].Details; d.Synopsis != "Plain words." || d.RuntimeMinutes != 601 || len(d.Genres) != 2 {
		t.Fatalf("details = %+v", d)
	}
	if d := snap.Items[1].Details; strings.Contains(d.Synopsis, "<") || !strings.HasPrefix(d.Synopsis, "First & best.") || !strings.Contains(d.Synopsis, "Second\nline") {
		t.Fatalf("an HTML description must become text: %q", d.Synopsis)
	}
}
