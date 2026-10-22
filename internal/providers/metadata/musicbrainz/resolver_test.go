package musicbrainz

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/evilperson1337/requestarr/internal/domain"
)

func TestCanResolveUsesExactHostAndHTTPSPort(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want bool
	}{
		{"https://musicbrainz.org/release/00000000-0000-0000-0000-000000000000", true},
		{"https://musicbrainz.org:443/release/00000000-0000-0000-0000-000000000000", true},
		{"https://evil.musicbrainz.org/release/00000000-0000-0000-0000-000000000000", false},
		{"https://musicbrainz.org:8443/release/00000000-0000-0000-0000-000000000000", false},
	} {
		u, _ := url.Parse(tc.raw)
		if got := (*Resolver)(nil).CanResolve(u); got != tc.want {
			t.Errorf("CanResolve(%q)=%v want %v", tc.raw, got, tc.want)
		}
	}
}

func TestResolveReleaseAndRecording(t *testing.T) {
	const id = "00000000-0000-0000-0000-000000000000"
	for _, tc := range []struct{ path, expected, media string }{{"/release/" + id, "/ws/2/release/" + id + "?fmt=json&inc=artists", "album"}, {"/recording/" + id, "/ws/2/recording/" + id + "?fmt=json&inc=artists", "album"}} {
		t.Run(tc.path, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.RequestURI() != tc.expected {
					t.Errorf("request URI=%q", r.URL.RequestURI())
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":"` + id + `","title":"Title","artist-credit":[{"name":"Artist"}]}`))
			}))
			defer s.Close()
			// Resolver API is fixed-origin; custom transport redirects the provider host to this deterministic fixture.
			client := s.Client()
			client.Transport = rewriteTransport{base: s.URL, next: http.DefaultTransport}
			r := New(client)
			u, _ := url.Parse("https://musicbrainz.org" + tc.path)
			item, err := r.Resolve(context.Background(), u)
			if err != nil {
				t.Fatal(err)
			}
			if item.Item.Title != "Artist — Title" || item.Item.MediaType != domain.MediaAlbum {
				t.Fatalf("resolved item=%#v", item.Item)
			}
		})
	}
}

type rewriteTransport struct {
	base string
	next http.RoundTripper
}

func (t rewriteTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	clone := r.Clone(r.Context())
	u := *r.URL
	fixture, _ := url.Parse(t.base)
	u.Scheme = fixture.Scheme
	u.Host = fixture.Host
	clone.URL = &u
	return t.next.RoundTrip(clone)
}
