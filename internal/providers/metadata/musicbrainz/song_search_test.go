package musicbrainz

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/providers/metadata"
)

// songClient answers every request with the recording search fixture.
func songClient(t *testing.T, status int, seen *[]*http.Request) *http.Client {
	t.Helper()
	fixture, err := os.ReadFile("testdata/search_recording.json")
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if seen != nil {
			*seen = append(*seen, r)
		}
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(string(fixture)))}, nil
	})}
}

// One recording is one row, however many releases it appears on, and the row
// names artist, length and the earliest release.
func TestSongSearchGivesOneRowPerRecording(t *testing.T) {
	var seen []*http.Request
	page, err := New(songClient(t, 200, &seen)).SongSearcher().Search(context.Background(), metadata.SearchQuery{Text: "money floyd", Page: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 {
		t.Fatalf("calls = %d, want one", len(seen))
	}
	u := seen[0].URL
	if u.Path != "/ws/2/recording" || u.Query().Get("fmt") != "json" || u.Query().Get("limit") != "25" || u.Query().Get("offset") != "25" {
		t.Fatalf("unexpected request %s", u)
	}
	if want := `(recording:"money" OR artist:"money") AND (recording:"floyd" OR artist:"floyd")`; u.Query().Get("query") != want {
		t.Fatalf("query = %s", u.Query().Get("query"))
	}
	if len(page.Results) != 3 || !page.HasMore {
		t.Fatalf("the malformed id is dropped and 25+4 of 31 leaves more: %+v", page)
	}
	first := page.Results[0]
	if first.MediaType != domain.MediaSong || first.Provider != "musicbrainz" || first.ProviderID != "b1a9c0e9-d987-4042-ae91-78d6a3267d69" ||
		first.Ref != "https://musicbrainz.org/recording/b1a9c0e9-d987-4042-ae91-78d6a3267d69" || first.Title != "Money" || first.Year != 1973 ||
		first.Subtitle != "Pink Floyd · 6:22 · The Dark Side of the Moon" ||
		first.ArtworkKind != domain.ArtworkSquare || first.ArtworkURL != "https://coverartarchive.org/release-group/f5093c06-23e3-404f-aeaa-40f72885ee3a/front-250" {
		t.Fatalf("unexpected first row: %+v", first)
	}
	if r := page.Results[1]; r.Subtitle != "Alpha feat. Beta" || r.Year != 2010 || r.ArtworkURL != "" {
		t.Fatalf("no length and no release: just the artists, got %+v", r)
	}
	if r := page.Results[2]; r.Subtitle != "Gamma · 1:05 · Only Release" || r.Year != 2020 {
		t.Fatalf("got %+v", r)
	}
}

func TestSongSearchQueryTreatsTextAsDataAndSkipsPunctuation(t *testing.T) {
	var seen []*http.Request
	s := New(songClient(t, 200, &seen)).SongSearcher()
	if _, err := s.Search(context.Background(), metadata.SearchQuery{Text: `a OR recording:x "q" \`, Page: 1}); err != nil {
		t.Fatal(err)
	}
	want := `(recording:"a" OR artist:"a") AND (recording:"OR" OR artist:"OR") AND (recording:"recording:x" OR artist:"recording:x") AND (recording:"\"q\"" OR artist:"\"q\"")`
	if got := seen[0].URL.Query().Get("query"); got != want {
		t.Fatalf("query = %s", got)
	}
	seen = nil
	page, err := s.Search(context.Background(), metadata.SearchQuery{Text: `* ?? "" :`, Page: 1})
	if err != nil || len(seen) != 0 || len(page.Results) != 0 {
		t.Fatalf("%+v %v after %d calls", page, err, len(seen))
	}
}

func TestEverySongFixtureRefIsAcceptedByTheResolver(t *testing.T) {
	provider := New(songClient(t, 200, nil))
	page, err := provider.SongSearcher().Search(context.Background(), metadata.SearchQuery{Text: "money", Page: 1})
	if err != nil || len(page.Results) == 0 {
		t.Fatalf("%+v %v", page, err)
	}
	for _, r := range page.Results {
		u, err := metadata.ParseURL(r.Ref)
		if err != nil || !provider.CanResolve(u) || u.Path != "/recording/"+r.ProviderID || !mbidPattern.MatchString(r.ProviderID) {
			t.Errorf("%s: resolver would not take ref %q (%v)", r.Title, r.Ref, err)
			continue
		}
		var requested string
		resolver := New(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if requested == "" {
				requested = req.URL.Path
			}
			body := `{"id":"` + r.ProviderID + `","title":"` + r.Title + `"}`
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
		})})
		item, err := resolver.Resolve(context.Background(), u)
		if err != nil || item.Item.ProviderID != r.ProviderID || item.Item.MediaType != domain.MediaSong || requested != "/ws/2/recording/"+r.ProviderID {
			t.Errorf("%s: resolve %v item %+v requested %s", r.Title, err, item.Item, requested)
		}
	}
}

func TestSongSearchFailuresAreErrorsNeverEmptyResults(t *testing.T) {
	var seen []*http.Request
	_, err := New(songClient(t, 503, &seen)).SongSearcher().Search(context.Background(), metadata.SearchQuery{Text: "money", Page: 1})
	if !errors.Is(err, metadata.ErrBusy) || len(seen) != 1 {
		t.Fatalf("err = %v after %d attempts", err, len(seen))
	}
}
