package musicbrainz

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/providers/metadata"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// searchClient answers every request with the release-group search fixture.
func searchClient(t *testing.T, status int, seen *[]*http.Request) *http.Client {
	t.Helper()
	fixture, err := os.ReadFile("testdata/search_release_group.json")
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

func TestSearchMapsReleaseGroupsToRows(t *testing.T) {
	var seen []*http.Request
	page, err := New(searchClient(t, 200, &seen)).Search(context.Background(), metadata.SearchQuery{Text: "dark side", Page: 2, IncludeSingles: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 {
		t.Fatalf("calls = %d, want one search call and no per-row lookups", len(seen))
	}
	u := seen[0].URL
	if u.Host != "musicbrainz.org" || u.Path != "/ws/2/release-group" || u.Query().Get("fmt") != "json" || u.Query().Get("limit") != "25" || u.Query().Get("offset") != "25" {
		t.Fatalf("unexpected request %s", u)
	}
	if !page.HasMore {
		t.Fatal("25 + 5 of 59 leaves more")
	}
	var got []string
	for _, r := range page.Results {
		got = append(got, r.Title+"|"+r.Subtitle)
	}
	if len(page.Results) != 4 {
		t.Fatalf("the row with a malformed id must be dropped, got %v", got)
	}
	first := page.Results[0]
	if first.MediaType != domain.MediaAlbum || first.Provider != "musicbrainz" || first.ProviderID != "f5093c06-23e3-404f-aeaa-40f72885ee3a" ||
		first.Ref != "https://musicbrainz.org/release-group/f5093c06-23e3-404f-aeaa-40f72885ee3a" ||
		first.Title != "The Dark Side of the Moon" || first.Year != 1973 || first.Subtitle != "Album · Pink Floyd" ||
		first.ArtworkKind != domain.ArtworkSquare || first.ArtworkURL != "https://coverartarchive.org/release-group/f5093c06-23e3-404f-aeaa-40f72885ee3a/front-250" {
		t.Fatalf("unexpected first row: %+v", first)
	}
	if r := page.Results[1]; r.Year != 1995 || r.Subtitle != "Album (Live) · Alpha & Beta" {
		t.Fatalf("secondary type and joined credits expected, got %+v", r)
	}
	if r := page.Results[2]; r.Subtitle != "Single · Gamma" {
		t.Fatalf("singles are labelled as such, got %+v", r)
	}
	if r := page.Results[3]; r.Year != 0 || r.Subtitle != "EP" {
		t.Fatalf("no date or artist: year 0 and just the type, got %+v", r)
	}
}

func luceneOf(t *testing.T, q metadata.SearchQuery) string {
	t.Helper()
	var seen []*http.Request
	if _, err := New(searchClient(t, 200, &seen)).Search(context.Background(), q); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 {
		t.Fatalf("calls = %d", len(seen))
	}
	return seen[0].URL.Query().Get("query")
}

func TestSearchQueryDefaultsToAlbumsAndTogglesSinglesAndEPs(t *testing.T) {
	const terms = `(releasegroup:"dark" OR artist:"dark") AND (releasegroup:"side" OR artist:"side")`
	if got, want := luceneOf(t, metadata.SearchQuery{Text: "  dark   side ", Page: 1}), terms+` AND primarytype:"album"`; got != want {
		t.Fatalf("query = %s\nwant    %s", got, want)
	}
	if got, want := luceneOf(t, metadata.SearchQuery{Text: "dark side", Page: 1, IncludeSingles: true}), terms+` AND (primarytype:"album" OR primarytype:"single" OR primarytype:"ep")`; got != want {
		t.Fatalf("query = %s\nwant    %s", got, want)
	}
}

// Typed punctuation and operator words are data, never query syntax.
func TestHostileQueryTextCannotChangeTheQuery(t *testing.T) {
	for _, tc := range []struct{ text, want string }{
		{`AC/DC: "Back" (in) *`, `(releasegroup:"AC/DC:" OR artist:"AC/DC:") AND (releasegroup:"\"Back\"" OR artist:"\"Back\"") AND (releasegroup:"(in)" OR artist:"(in)") AND primarytype:"album"`},
		{`a OR primarytype:single`, `(releasegroup:"a" OR artist:"a") AND (releasegroup:"OR" OR artist:"OR") AND (releasegroup:"primarytype:single" OR artist:"primarytype:single") AND primarytype:"album"`},
		{`back\ slash" ) OR (`, `(releasegroup:"back\\" OR artist:"back\\") AND (releasegroup:"slash\"" OR artist:"slash\"") AND (releasegroup:"OR" OR artist:"OR")` + ` AND primarytype:"album"`},
		{`NOT -x`, `(releasegroup:"NOT" OR artist:"NOT") AND (releasegroup:"-x" OR artist:"-x") AND primarytype:"album"`},
	} {
		if got := luceneOf(t, metadata.SearchQuery{Text: tc.text, Page: 1}); got != tc.want {
			t.Errorf("%q\n got %s\nwant %s", tc.text, got, tc.want)
		}
	}
}

func TestSearchOfOnlyPunctuationAsksNothing(t *testing.T) {
	var seen []*http.Request
	page, err := New(searchClient(t, 200, &seen)).Search(context.Background(), metadata.SearchQuery{Text: `* ?? "" :`, Page: 1})
	if err != nil || len(seen) != 0 || len(page.Results) != 0 {
		t.Fatalf("%+v %v after %d calls", page, err, len(seen))
	}
}

// Picking a result must run the resolver unchanged: every ref a search can
// produce has to be one the MusicBrainz resolver takes, and it asks for the
// same entity the row names.
func TestEveryFixtureResultRefIsAcceptedByTheResolver(t *testing.T) {
	provider := New(searchClient(t, 200, nil))
	page, err := provider.Search(context.Background(), metadata.SearchQuery{Text: "dark", Page: 1, IncludeSingles: true})
	if err != nil || len(page.Results) == 0 {
		t.Fatalf("%+v %v", page, err)
	}
	for _, r := range page.Results {
		u, err := metadata.ParseURL(r.Ref)
		if err != nil || !provider.CanResolve(u) || u.Path != "/release-group/"+r.ProviderID || !mbidPattern.MatchString(r.ProviderID) {
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
		if err != nil || item.Item.ProviderID != r.ProviderID || item.Item.MediaType != domain.MediaAlbum || requested != "/ws/2/release-group/"+r.ProviderID {
			t.Errorf("%s: resolve %v item %+v requested %s", r.Title, err, item.Item, requested)
		}
	}
}

// MusicBrainz answers "busy" now and then even inside its rate limit, so a
// busy answer is asked again, a bounded number of times, and is an error
// (never an empty list) when it stays busy. Other failures are not retried.
func TestSearchFailuresAreErrorsNeverEmptyResults(t *testing.T) {
	for status, want := range map[int]error{429: metadata.ErrBusy, 503: metadata.ErrBusy, 502: metadata.ErrBusy, 500: nil} {
		var seen []*http.Request
		_, err := New(searchClient(t, status, &seen)).Search(context.Background(), metadata.SearchQuery{Text: "dark", Page: 1})
		if err == nil || want != nil && !errors.Is(err, want) {
			t.Errorf("status %d: err = %v, want %v", status, err, want)
		}
		attempts := searchAttempts
		if want == nil {
			attempts = 1
		}
		if len(seen) != attempts {
			t.Errorf("status %d: %d attempts, want %d", status, len(seen), attempts)
		}
	}
}

// A busy answer followed by a good one is a good search, and the retry waited
// for its own slot in the spacer: it never spends the budget early.
func TestABusySearchIsAskedAgainInItsOwnSlot(t *testing.T) {
	clock := newFakeClock()
	saved := apiSpacer
	apiSpacer = &spacer{interval: time.Second, clock: clock}
	defer func() { apiSpacer = saved }()
	fixture, err := os.ReadFile("testdata/search_release_group.json")
	if err != nil {
		t.Fatal(err)
	}
	var attempts []time.Time
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		attempts = append(attempts, clock.Now())
		status := 200
		if len(attempts) == 1 {
			status = 503
		}
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(string(fixture)))}, nil
	})}
	done := make(chan error, 1)
	go func() {
		_, err := New(client).Search(context.Background(), metadata.SearchQuery{Text: "dark", Page: 1})
		done <- err
	}()
	waitForQueue(t, apiSpacer, 1) // the retry is waiting for the next slot
	clock.Advance(time.Second)
	if err := <-done; err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(attempts) != 2 || attempts[1].Sub(attempts[0]) < time.Second {
		t.Fatalf("attempts at %v; the retry must wait a full interval", attempts)
	}
}

func TestSearchGivesUpItsSlotWhenCancelled(t *testing.T) {
	clock := newFakeClock()
	saved := apiSpacer
	apiSpacer = &spacer{interval: time.Second, clock: clock}
	defer func() { apiSpacer = saved }()
	_ = apiSpacer.wait(context.Background())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := New(searchClient(t, 200, nil)).Search(ctx, metadata.SearchQuery{Text: "dark", Page: 1})
		done <- err
	}()
	waitForQueue(t, apiSpacer, 1)
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	waitForQueue(t, apiSpacer, 0)
}
