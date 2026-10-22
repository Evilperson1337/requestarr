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
