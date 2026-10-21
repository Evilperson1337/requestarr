package jellyfin

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

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
	items, err := c.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 501 || requests != 2 {
		t.Fatalf("got %d items over %d requests", len(items), requests)
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
			items, err := c.Snapshot(context.Background())
			if tt.name == "empty complete" {
				if err != nil || len(items) != 0 {
					t.Fatalf("items=%v err=%v", items, err)
				}
				return
			}
			if err == nil || items != nil {
				t.Fatalf("partial snapshot returned: %v, %v", items, err)
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
