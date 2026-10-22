package musicbrainz

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/domain"
)

const (
	group  = "11111111-1111-1111-1111-111111111111"
	rec    = "22222222-2222-2222-2222-222222222222"
	relA   = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	relB   = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	relC   = "cccccccc-cccc-cccc-cccc-cccccccccccc"
	trackA = "a1a1a1a1-a1a1-a1a1-a1a1-a1a1a1a1a1a1"
	trackB = "b1b1b1b1-b1b1-b1b1-b1b1-b1b1b1b1b1b1"
	other  = "99999999-9999-9999-9999-999999999999"
)

type mbServer struct {
	mu       sync.Mutex
	requests []*http.Request
	times    []time.Time
}

func (m *mbServer) handler(respond func(uri string) (int, string)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		m.requests = append(m.requests, r.Clone(context.Background()))
		m.times = append(m.times, time.Now())
		m.mu.Unlock()
		status, body := respond(r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
}

func newAliaser(t *testing.T, interval time.Duration, respond func(uri string) (int, string)) (*Aliaser, *mbServer) {
	t.Helper()
	m := &mbServer{}
	s := httptest.NewServer(m.handler(respond))
	t.Cleanup(s.Close)
	client := s.Client()
	client.Transport = rewriteTransport{base: s.URL, next: http.DefaultTransport}
	return NewAliaser(client, interval), m
}

func sorted(in []string) []string { out := append([]string(nil), in...); sort.Strings(out); return out }

func TestAlbumAliasesAreTheReleasesOfItsReleaseGroupAcrossPages(t *testing.T) {
	a, m := newAliaser(t, 0, func(uri string) (int, string) {
		switch {
		case strings.Contains(uri, "offset=0"):
			return 200, `{"release-count":3,"release-offset":0,"releases":[{"id":"` + relA + `"},{"id":"` + relB + `"}]}`
		case strings.Contains(uri, "offset=2"):
			return 200, `{"release-count":3,"release-offset":2,"releases":[{"id":"` + relC + `"}]}`
		}
		return 404, `{}`
	})
	got, err := a.Aliases(context.Background(), domain.CatalogRef{ID: "c", MediaType: domain.MediaAlbum, Provider: "musicbrainz", ProviderID: group})
	if err != nil {
		t.Fatal(err)
	}
	want := sorted([]string{group, relA, relB, relC})
	if g := sorted(got); strings.Join(g, ",") != strings.Join(want, ",") {
		t.Fatalf("aliases=%v want %v", g, want)
	}
	if len(m.requests) != 2 || !strings.HasPrefix(m.requests[0].URL.Path, "/ws/2/release") || m.requests[0].URL.Query().Get("release-group") != group || m.requests[0].URL.Query().Get("fmt") != "json" {
		t.Fatalf("requests=%v", m.requests)
	}
	if ua := m.requests[0].Header.Get("User-Agent"); !strings.HasPrefix(ua, "Requestarr/") {
		t.Fatalf("MusicBrainz requires a meaningful User-Agent, got %q", ua)
	}
}

func TestSongAliasesAreTheTracksCarryingTheRecordingOnItsReleases(t *testing.T) {
	a, m := newAliaser(t, 0, func(uri string) (int, string) {
		return 200, `{"release-count":2,"release-offset":0,"releases":[
		 {"id":"` + relA + `","media":[{"tracks":[{"id":"` + trackA + `","recording":{"id":"` + rec + `"}},{"id":"` + other + `","recording":{"id":"` + other + `"}}]}]},
		 {"id":"` + relB + `","media":[{"tracks":[{"id":"` + trackB + `","recording":{"id":"` + rec + `"}}]}]}]}`
	})
	got, err := a.Aliases(context.Background(), domain.CatalogRef{ID: "c", MediaType: domain.MediaSong, Provider: "musicbrainz", ProviderID: rec})
	if err != nil {
		t.Fatal(err)
	}
	want := sorted([]string{rec, trackA, trackB})
	if g := sorted(got); strings.Join(g, ",") != strings.Join(want, ",") {
		t.Fatalf("aliases=%v want %v (other recordings' tracks must not match)", g, want)
	}
	q := m.requests[0].URL.Query()
	if q.Get("recording") != rec || q.Get("inc") != "recordings" {
		t.Fatalf("query=%v", q)
	}
}

func TestLookupsRespectTheOneRequestPerSecondRule(t *testing.T) {
	a, m := newAliaser(t, 60*time.Millisecond, func(uri string) (int, string) {
		if strings.Contains(uri, "offset=0") {
			return 200, `{"release-count":2,"releases":[{"id":"` + relA + `"}]}`
		}
		return 200, `{"release-count":2,"releases":[{"id":"` + relB + `"}]}`
	})
	if _, err := a.Aliases(context.Background(), domain.CatalogRef{MediaType: domain.MediaAlbum, ProviderID: group}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Aliases(context.Background(), domain.CatalogRef{MediaType: domain.MediaAlbum, ProviderID: group}); err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(m.times); i++ {
		if gap := m.times[i].Sub(m.times[i-1]); gap < 55*time.Millisecond {
			t.Fatalf("requests %d and %d only %v apart", i-1, i, gap)
		}
	}
	if len(m.times) < 3 {
		t.Fatalf("only %d requests", len(m.times))
	}
}

func TestAliasLookupFailuresReturnNoAliases(t *testing.T) {
	ref := domain.CatalogRef{MediaType: domain.MediaAlbum, ProviderID: group}
	for name, respond := range map[string]func(string) (int, string){
		"server error": func(string) (int, string) { return 503, `{}` },
		"malformed":    func(string) (int, string) { return 200, `{"releases":` },
		"second page fails": func(uri string) (int, string) {
			if strings.Contains(uri, "offset=0") {
				return 200, `{"release-count":3,"releases":[{"id":"` + relA + `"},{"id":"` + relB + `"}]}`
			}
			return 500, `{}`
		},
		"count lies about size": func(string) (int, string) { return 200, `{"release-count":5,"releases":[]}` },
	} {
		t.Run(name, func(t *testing.T) {
			a, _ := newAliaser(t, 0, respond)
			if got, err := a.Aliases(context.Background(), ref); err == nil || len(got) != 0 {
				t.Fatalf("aliases=%v err=%v: a partial lookup must not be returned", got, err)
			}
		})
	}
	a, _ := newAliaser(t, 0, func(string) (int, string) { return 200, `{"release-count":1,"releases":[{"id":"` + relA + `"}]}` })
	for _, bad := range []domain.CatalogRef{{MediaType: domain.MediaMovie, ProviderID: group}, {MediaType: domain.MediaAlbum, ProviderID: "not-a-uuid"}} {
		if _, err := a.Aliases(context.Background(), bad); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.Aliases(ctx, ref); err == nil {
		t.Error("cancelled context accepted")
	}
}

func TestInvalidIdentifiersInResponsesAreDropped(t *testing.T) {
	a, _ := newAliaser(t, 0, func(string) (int, string) {
		return 200, `{"release-count":2,"releases":[{"id":"` + strings.ToUpper(relA) + `"},{"id":"<script>"}]}`
	})
	got, err := a.Aliases(context.Background(), domain.CatalogRef{MediaType: domain.MediaAlbum, ProviderID: group})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(sorted(got), ",") != strings.Join(sorted([]string{group, relA}), ",") {
		t.Fatalf("aliases=%v, want only the group and the lower-cased valid release", got)
	}
}

func TestAnOverlyLargeReleaseSetIsCappedNotFailed(t *testing.T) {
	var pages int
	a, _ := newAliaser(t, 0, func(uri string) (int, string) {
		pages++
		id := []string{relA, relB, relC, trackA, trackB, other}[pages%6]
		return 200, `{"release-count":100000,"releases":[{"id":"` + id + `"}]}`
	})
	got, err := a.Aliases(context.Background(), domain.CatalogRef{MediaType: domain.MediaAlbum, ProviderID: group})
	if err != nil || len(got) < 2 {
		t.Fatalf("aliases=%v err=%v: a huge release group should still yield what was read", got, err)
	}
	if pages != maxAliasPages {
		t.Fatalf("read %d pages, want the cap of %d", pages, maxAliasPages)
	}
}
