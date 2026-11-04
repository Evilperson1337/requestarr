package romm

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

const testToken = "secret-romm-token"

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// backend serves the recorded-shape fixtures, bending them per scenario.
type backend struct {
	t        *testing.T
	scenario inventorytest.Scenario
	romsBody string // overrides the ROM page for ad-hoc tests
	mu       sync.Mutex
	requests []*http.Request
}

func (b *backend) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	b.requests = append(b.requests, r.Clone(context.Background()))
	b.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer "+testToken || b.scenario == inventorytest.Unauthorized {
		http.Error(w, "unauthorized "+testToken, http.StatusUnauthorized) // a hostile body that echoes the token
		return
	}
	if b.scenario == inventorytest.ServerError {
		http.Error(w, "boom", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	offset := r.URL.Query().Get("offset")
	switch {
	case r.URL.Path == "/api/platforms":
		_, _ = w.Write(fixture(b.t, "platforms.json"))
	case r.URL.Path != "/api/roms":
		http.NotFound(w, r)
	case b.romsBody != "":
		_, _ = w.Write([]byte(b.romsBody))
	case b.scenario == inventorytest.Empty:
		_, _ = w.Write(fixture(b.t, "roms_empty.json"))
	case b.scenario == inventorytest.Malformed:
		_, _ = w.Write(fixture(b.t, "roms_malformed.json"))
	case b.scenario == inventorytest.Truncated:
		_, _ = w.Write(fixture(b.t, "roms_truncated.json"))
	case offset == "0":
		_, _ = w.Write(fixture(b.t, "roms_page1.json"))
	case offset == "2" && b.scenario == inventorytest.MidwayFailure:
		http.Error(w, "boom", http.StatusInternalServerError)
	case offset == "2":
		_, _ = w.Write(fixture(b.t, "roms_page2.json"))
	default:
		http.NotFound(w, r)
	}
}

func newBackendClient(t *testing.T, b *backend) *Client {
	t.Helper()
	b.t = t
	ts := httptest.NewServer(b)
	t.Cleanup(ts.Close)
	c, err := NewWithHTTPClient(ts.URL, testToken, ts.Client())
	if err != nil {
		t.Fatal(err)
	}
	c.pageSize = 2 // the fixtures are recorded at page size 2
	return c
}

func TestRommPassesTheInventorySourceContract(t *testing.T) {
	inventorytest.Run(t, func(t *testing.T, s inventorytest.Scenario) inventory.Source {
		return newBackendClient(t, &backend{scenario: s})
	}, inventorytest.Expectations{LibraryItems: 3, Secret: testToken})
}

func TestRommIsAuthoritativeOnlyForGamesIdentifiedByIGDB(t *testing.T) {
	d := newBackendClient(t, &backend{}).Descriptor()
	if len(d.MediaTypes) != 1 || d.MediaTypes[0] != domain.MediaGame || len(d.IdentifierKinds) != 1 || d.IdentifierKinds[0] != "igdb" || d.Kind != "romm" {
		t.Fatalf("descriptor=%+v", d)
	}
}

func TestSnapshotMapsPlatformsToIGDBKeysAndSkipsFilesMissingFromDisk(t *testing.T) {
	b := &backend{}
	snap, err := newBackendClient(t, b).Snapshot(context.Background())
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
	chrono := byID["11"]
	if chrono.MediaType != domain.MediaGame || chrono.Title != "Chrono Trigger" || chrono.PlatformKey != "igdb:19" || chrono.ExternalIDs["igdb"] != "1035" {
		t.Fatalf("chrono=%+v", chrono)
	}
	if hades := byID["12"]; hades.PlatformKey != "igdb:130" || hades.ExternalIDs["igdb"] != "113112" {
		t.Fatalf("hades=%+v", hades)
	}
	// A platform RomM could not match to IGDB yields no key; a ROM RomM could not match has no igdb id.
	if hb := byID["13"]; hb.PlatformKey != "" || len(hb.ExternalIDs) != 0 {
		t.Fatalf("homebrew=%+v", hb)
	}
	if _, present := byID["14"]; present {
		t.Fatal("a ROM whose file is missing from disk counted as inventory")
	}
}

func TestTokenTravelsOnlyInTheAuthorizationHeader(t *testing.T) {
	b := &backend{}
	if _, err := newBackendClient(t, b).Snapshot(context.Background()); err != nil {
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

func TestInconsistentRomResponsesFailInsteadOfGuessing(t *testing.T) {
	for name, body := range map[string]string{
		"older RomM answering with a bare array": string(fixture(t, "roms_legacy_array.json")),
		"rom on a platform RomM never listed":    string(fixture(t, "roms_unknown_platform.json")),
		"missing total":                          `{"items":[],"limit":2,"offset":0}`,
		"rom without an id":                      `{"items":[{"name":"x","igdb_id":1,"platform_id":1}],"total":1,"limit":2,"offset":0}`,
		"rom without a name":                     `{"items":[{"id":1,"igdb_id":1,"platform_id":1}],"total":1,"limit":2,"offset":0}`,
		"page claims a different offset":         `{"items":[{"id":1,"name":"x","platform_id":1}],"total":1,"limit":2,"offset":5}`,
	} {
		t.Run(name, func(t *testing.T) {
			snap, err := newBackendClient(t, &backend{romsBody: body}).Snapshot(context.Background())
			if err == nil || snap.Complete || len(snap.Items) != 0 {
				t.Fatalf("err=%v complete=%v items=%d", err, snap.Complete, len(snap.Items))
			}
		})
	}
}

func TestNewRejectsUnsafeConfiguration(t *testing.T) {
	for _, tc := range []struct{ url, token string }{
		{"", "t"}, {"ftp://romm:3000", "t"}, {"http://u:p@romm:3000", "t"}, {"http://romm:3000/api", "t"}, {"http://romm:3000?x=1", "t"}, {"http://romm:3000", ""},
	} {
		if _, err := New(tc.url, tc.token); err == nil {
			t.Errorf("New(%q,%q) accepted", tc.url, tc.token)
		}
	}
	if _, err := New("https://romm.example", "t"); err != nil {
		t.Fatal(err)
	}
}

func TestGamesCarryTheirAddedDatePlatformNameAndAWellFormedCover(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/platforms" {
			_, _ = w.Write([]byte(`[{"id":1,"igdb_id":6}]`))
			return
		}
		_, _ = w.Write([]byte(`{"total":2,"offset":0,"items":[
			{"id":10,"name":"Good Game","igdb_id":99,"platform_id":1,"created_at":"2026-10-11T07:42:15+00:00","path_cover_large":"/assets/romm/resources/roms/1/10/cover/big.png?ts=2026-10-11 07:42:17","platform_display_name":"Windows"},
			{"id":11,"name":"Odd Game","platform_id":1,"created_at":"not a date","path_cover_large":"http://evil.example/cover.png","platform_display_name":"  "}]}`))
	}))
	defer server.Close()
	c, err := NewWithHTTPClient(server.URL, testToken, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	snap, err := c.Snapshot(context.Background())
	if err != nil || len(snap.Items) != 2 {
		t.Fatalf("snapshot %v %d", err, len(snap.Items))
	}
	good := snap.Items[0]
	if !strings.HasSuffix(good.ExternalURL, "/rom/10") {
		t.Fatalf("link = %q", good.ExternalURL)
	}
	if good.AddedAt.Year() != 2026 || good.ArtworkRef != "/assets/romm/resources/roms/1/10/cover/big.png" || good.Detail != "Windows" {
		t.Fatalf("good = %+v", good)
	}
	odd := snap.Items[1]
	if !odd.AddedAt.IsZero() || odd.ArtworkRef != "" || odd.Detail != "" {
		t.Fatalf("a bad date, a foreign cover URL and a blank platform must all be dropped: %+v", odd)
	}
}

func TestArtworkIsFetchedWithTheTokenInAHeaderAndOnlyForAWellFormedReference(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+testToken || r.URL.RawQuery != "" {
			http.Error(w, "bad", http.StatusUnauthorized)
			return
		}
		if strings.Contains(r.URL.Path, "/99/") {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<html>"))
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("pngbytes"))
	}))
	defer server.Close()
	c, _ := NewWithHTTPClient(server.URL, testToken, server.Client())
	got, err := c.Artwork(context.Background(), "/assets/romm/resources/roms/1/10/cover/big.png")
	if err != nil || got.ContentType != "image/png" || string(got.Data) != "pngbytes" {
		t.Fatalf("artwork %+v err %v", got, err)
	}
	for _, bad := range []string{"/api/users", "http://evil.example/x.png", "/assets/romm/resources/roms/1/10/cover/big.png?x=1", "/assets/romm/resources/roms/../../etc/passwd", "/assets/romm/resources/roms/1/10/manual.pdf"} {
		if _, err := c.Artwork(context.Background(), bad); err == nil {
			t.Errorf("%q must be refused before any request", bad)
		}
	}
	if _, err := c.Artwork(context.Background(), "/assets/romm/resources/roms/1/99/cover/big.png"); err == nil {
		t.Fatal("a non-image answer must be refused")
	}
}

func TestGamesCarryRomMsSummaryRatingAndGenres(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/platforms" {
			_, _ = w.Write([]byte(`[{"id":1,"igdb_id":6}]`))
			return
		}
		_, _ = w.Write([]byte(`{"total":3,"offset":0,"items":[
			{"id":10,"name":"A","platform_id":1,"summary":"A quest.","igdb_metadata":{"total_rating":"87.46","genres":["RPG"]}},
			{"id":11,"name":"B","platform_id":1,"igdb_metadata":{"total_rating":70}},
			{"id":12,"name":"C","platform_id":1,"igdb_metadata":null}]}`))
	}))
	defer server.Close()
	c, err := NewWithHTTPClient(server.URL, testToken, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	snap, err := c.Snapshot(context.Background())
	if err != nil || len(snap.Items) != 3 {
		t.Fatalf("snapshot %v %d", err, len(snap.Items))
	}
	if d := snap.Items[0].Details; d.Synopsis != "A quest." || d.Rating != 8.7 || len(d.Genres) != 1 {
		t.Fatalf("details = %+v", d)
	}
	if d := snap.Items[1].Details; d.Rating != 7 {
		t.Fatalf("a numeric rating must work too: %+v", d)
	}
	if !snap.Items[2].Details.Empty() {
		t.Fatalf("no metadata means no details: %+v", snap.Items[2].Details)
	}
}
