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
		{"https://musicbrainz.org/release-group/00000000-0000-0000-0000-000000000000", true},
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

func TestResolveUsesReleaseGroupForAlbumsAndRecordingForSongs(t *testing.T) {
	const id = "00000000-0000-0000-0000-000000000000"
	const group = "11111111-1111-1111-1111-111111111111"
	for _, tc := range []struct {
		name, path, expectedURI, body string
		wantMedia                     domain.MediaType
		wantProviderID, wantURL       string
		wantTitle                     string
		wantYear                      int
	}{
		{"release-group", "/release-group/" + id, "/ws/2/release-group/" + id + "?fmt=json&inc=artists+genres+ratings",
			`{"id":"` + id + `","title":"Title","first-release-date":"1999-05-01","artist-credit":[{"name":"Artist"}]}`,
			domain.MediaAlbum, id, "https://musicbrainz.org/release-group/" + id, "Artist — Title", 1999},
		{"recording", "/recording/" + id, "/ws/2/recording/" + id + "?fmt=json&inc=artists+genres+ratings+releases",
			`{"id":"` + id + `","title":"Song","first-release-date":"2001-02-03","artist-credit":[{"name":"Artist"}]}`,
			domain.MediaSong, id, "https://musicbrainz.org/recording/" + id, "Artist — Song", 2001},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.RequestURI() != tc.expectedURI {
					t.Errorf("request URI=%q want %q", r.URL.RequestURI(), tc.expectedURI)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.body))
			}))
			defer s.Close()
			// Resolver API is fixed-origin; custom transport redirects the provider host to this deterministic fixture.
			client := s.Client()
			client.Transport = rewriteTransport{base: s.URL, next: http.DefaultTransport}
			u, _ := url.Parse("https://musicbrainz.org" + tc.path)
			item, err := New(client).Resolve(context.Background(), u)
			if err != nil {
				t.Fatal(err)
			}
			got := item.Item
			if got.MediaType != tc.wantMedia || got.Provider != "musicbrainz" || got.ProviderID != tc.wantProviderID || got.ExternalURL != tc.wantURL || got.Title != tc.wantTitle || got.Year != tc.wantYear {
				t.Fatalf("resolved item=%#v", got)
			}
		})
	}
}

func TestResolveRejectsReleaseWithoutAGroupAndUnsupportedEntities(t *testing.T) {
	const id = "00000000-0000-0000-0000-000000000000"
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"` + id + `","title":"No group"}`))
	}))
	defer s.Close()
	client := s.Client()
	client.Transport = rewriteTransport{base: s.URL, next: http.DefaultTransport}
	for _, path := range []string{"/release/" + id, "/artist/" + id, "/work/" + id, "/release-group/not-a-uuid"} {
		u, _ := url.Parse("https://musicbrainz.org" + path)
		if item, err := New(client).Resolve(context.Background(), u); err == nil {
			t.Errorf("%s resolved to %#v", path, item.Item)
		}
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
