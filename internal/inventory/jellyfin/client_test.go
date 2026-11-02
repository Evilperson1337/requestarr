package jellyfin

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/evilperson1337/requestarr/internal/inventory"
	"github.com/evilperson1337/requestarr/internal/inventory/inventorytest"
)

const testAPIKey = "jellyfin-test-key"

func TestJellyfinPassesTheInventorySourceContract(t *testing.T) {
	inventorytest.Run(t, func(t *testing.T, scenario inventorytest.Scenario) inventory.Source {
		t.Helper()
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Emby-Token") != testAPIKey {
				http.Error(w, "unauthorized "+testAPIKey, http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			switch scenario {
			case inventorytest.ServerError, inventorytest.MidwayFailure:
				http.Error(w, "failure", http.StatusInternalServerError)
			case inventorytest.Unauthorized:
				http.Error(w, "unauthorized "+testAPIKey, http.StatusUnauthorized)
			case inventorytest.Malformed:
				_, _ = w.Write([]byte("{"))
			case inventorytest.Truncated:
				_, _ = w.Write([]byte(`{"StartIndex":0,"TotalRecordCount":2,"Items":[{"Id":"movie-1","Type":"Movie","Name":"Film"}]}`))
			case inventorytest.Empty:
				writeFixture(t, w, "empty.json")
			default:
				writeFixture(t, w, "library.json")
			}
		}))
		t.Cleanup(ts.Close)
		client, err := NewWithHTTPClient(ts.URL, testAPIKey, "configured-jellyfin-source", ts.Client())
		if err != nil {
			t.Fatal(err)
		}
		return client
	}, inventorytest.Expectations{LibraryItems: 2, Secret: testAPIKey})
}

func writeFixture(t *testing.T, w http.ResponseWriter, name string) {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write(body)
}

func TestSnapshotPaginationAndAuth(t *testing.T) {
	requests := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		requests++
		if r.Header.Get("X-Emby-Token") != "secret" {
			t.Errorf("missing auth header")
		}
		if r.URL.Query().Get("StartIndex") != fmt.Sprint((requests-1)*pageSize) {
			t.Errorf("unexpected offset %s", r.URL.Query().Get("StartIndex"))
		}
		start := (requests - 1) * pageSize
		if requests == 1 {
			_, _ = fmt.Fprintf(w, `{"StartIndex":%d,"TotalRecordCount":501,"Items":[`, start)
			for i := 0; i < pageSize; i++ {
				if i > 0 {
					_, _ = w.Write([]byte(","))
				}
				_, _ = fmt.Fprintf(w, `{"Id":"%d","Type":"Movie","Name":"Film","ProviderIds":{"Tmdb":"42"}}`, i)
			}
			_, _ = w.Write([]byte("]}"))
			return
		}
		_, _ = fmt.Fprintf(w, `{"StartIndex":%d,"TotalRecordCount":501,"Items":[{"Id":"last","Type":"Series","Name":"Show"}]}`, start)
	}))
	defer ts.Close()
	c, err := NewWithHTTPClient(ts.URL, "secret", "source", ts.Client())
	if err != nil {
		t.Fatal(err)
	}
	snap, err := c.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !snap.Complete || snap.SourceID != "source" || len(snap.Items) != 501 || requests != 2 {
		t.Fatalf("snapshot=%+v over %d requests", snap, requests)
	}
	if want := ts.URL + "/web/index.html#!/details?id=0"; snap.Items[0].ExternalURL != want {
		t.Fatalf("link = %q, want %q", snap.Items[0].ExternalURL, want)
	}
}

func TestSnapshotFailuresNeverReturnPartial(t *testing.T) {
	tests := []struct {
		name, body  string
		status      int
		contentType string
	}{
		{"empty complete", `{"StartIndex":0,"TotalRecordCount":0,"Items":[]}`, 200, ""},
		{"empty object", `{}`, 200, ""},
		{"missing items", `{"StartIndex":0,"TotalRecordCount":0}`, 200, ""},
		{"null items", `{"StartIndex":0,"TotalRecordCount":0,"Items":null}`, 200, ""},
		{"missing start", `{"TotalRecordCount":0,"Items":[]}`, 200, ""},
		{"null start", `{"StartIndex":null,"TotalRecordCount":0,"Items":[]}`, 200, ""},
		{"missing total", `{"StartIndex":0,"Items":[]}`, 200, ""},
		{"null total", `{"StartIndex":0,"TotalRecordCount":null,"Items":[]}`, 200, ""},
		{"malformed shape", `{"StartIndex":[],"TotalRecordCount":0,"Items":[]}`, 200, ""},
		{"wrong content type", `{"StartIndex":0,"TotalRecordCount":0,"Items":[]}`, 200, "text/html"},
		{"inconsistent total", `{"StartIndex":500,"TotalRecordCount":501,"Items":[{"Id":"x","Type":"Movie"}]}`, 200, ""},
		{"oversized page", `{"StartIndex":0,"TotalRecordCount":1,"Items":[{"Id":"x","Type":"Movie"},{"Id":"y","Type":"Movie"}]}`, 200, ""},
		{"server error no secret", ``, 500, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			call := 0
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				call++
				if tt.status != 200 {
					w.WriteHeader(tt.status)
					_, _ = w.Write([]byte("secret"))
					return
				}
				contentType := tt.contentType
				if contentType == "" {
					contentType = "application/json; charset=utf-8"
				}
				w.Header().Set("Content-Type", contentType)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer ts.Close()
			c, err := NewWithHTTPClient(ts.URL, "secret", "source", ts.Client())
			if err != nil {
				t.Fatal(err)
			}
			snap, err := c.Snapshot(context.Background())
			if tt.name == "empty complete" {
				if err != nil || !snap.Complete || len(snap.Items) != 0 {
					t.Fatalf("snapshot=%+v err=%v", snap, err)
				}
				return
			}
			if err == nil || snap.Complete || len(snap.Items) != 0 {
				t.Fatalf("partial snapshot returned: %+v, %v", snap, err)
			}
			if strings.Contains(err.Error(), "secret") {
				t.Fatal("error leaked key")
			}
		})
	}
}

func TestNewRejectsUnsafeBaseURL(t *testing.T) {
	for _, raw := range []string{"http://user:pass@example.test", "http://example.test/path", "http://example.test/?q=x", "http://example.test/#x"} {
		if _, err := New(raw, "key", "id"); err == nil {
			t.Errorf("accepted %q", raw)
		}
	}
}

func TestDescriptorDeclaresMovieAndTVWithTMDBIdentifiers(t *testing.T) {
	c, err := NewWithHTTPClient("http://jellyfin.local:8096", "secret", "source-id", http.DefaultClient)
	if err != nil {
		t.Fatal(err)
	}
	d := c.Descriptor()
	if d.ID != "source-id" || d.Kind != "jellyfin" || len(d.MediaTypes) != 2 || len(d.IdentifierKinds) != 1 || d.IdentifierKinds[0] != "tmdb" {
		t.Fatalf("descriptor=%+v", d)
	}
}
