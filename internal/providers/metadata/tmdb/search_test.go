package tmdb_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/providers/metadata"
	"github.com/evilperson1337/requestarr/internal/providers/metadata/tmdb"
)

func searchClient(t *testing.T, status int, seen *[]*http.Request) *http.Client {
	t.Helper()
	fixture, err := os.ReadFile("testdata/search_multi.json")
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

func TestSearchMapsMoviesAndTVAndDropsEverythingElse(t *testing.T) {
	var seen []*http.Request
	page, err := tmdb.New("token", searchClient(t, 200, &seen)).Search(context.Background(), metadata.SearchQuery{Text: `dune: "part" & more`, Page: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 {
		t.Fatalf("calls = %d, want one multi-search call", len(seen))
	}
	req := seen[0]
	if req.URL.Host != "api.themoviedb.org" || req.URL.Path != "/3/search/multi" || req.Header.Get("Authorization") != "Bearer token" {
		t.Fatalf("unexpected request %s", req.URL)
	}
	if q := req.URL.Query(); q.Get("query") != `dune: "part" & more` || q.Get("page") != "2" || q.Get("include_adult") != "false" {
		t.Fatalf("query not carried intact: %v", q)
	}
	if !page.HasMore {
		t.Fatal("page 2 of 3 has more")
	}
	var got []string
	for _, r := range page.Results {
		got = append(got, string(r.MediaType)+":"+r.ProviderID+":"+r.Title)
	}
	want := []string{"movie:438631:Dune", "tv:90000:Dune: Prophecy", "movie:841:Dune", "movie:5:Undated and unposted"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("results = %v, want %v", got, want)
	}
	first := page.Results[0]
	if first.Year != 2021 || first.Provider != "tmdb" || first.ArtworkKind != domain.ArtworkPoster ||
		first.ArtworkURL != "https://image.tmdb.org/t/p/w500/d5NXSklXo0qyIYkgV94XAgMIckC.jpg" {
		t.Fatalf("unexpected first result: %+v", first)
	}
	if page.Results[2].ArtworkURL != "" || page.Results[3].Year != 0 || page.Results[3].ArtworkURL != "" {
		t.Fatalf("missing or unsafe artwork/date must be empty: %+v %+v", page.Results[2], page.Results[3])
	}
}

func TestSearchLastPageHasNoMore(t *testing.T) {
	// The fixture says page 1 of 3, so asking for page 3 reports the end.
	page, err := tmdb.New("token", searchClient(t, 200, nil)).Search(context.Background(), metadata.SearchQuery{Text: "dune", Page: 3})
	if err != nil || page.HasMore {
		t.Fatalf("%+v %v", page, err)
	}
}

// Picking a result must run the resolver unchanged, so every ref a search can
// produce has to be one the TMDB resolver accepts.
var refPath = regexp.MustCompile(`^/(movie|tv)/[0-9]+$`)

func TestEveryFixtureResultRefIsAcceptedByTheResolver(t *testing.T) {
	provider := tmdb.New("token", searchClient(t, 200, nil))
	page, err := provider.Search(context.Background(), metadata.SearchQuery{Text: "dune", Page: 1})
	if err != nil || len(page.Results) == 0 {
		t.Fatalf("%+v %v", page, err)
	}
	for _, r := range page.Results {
		u, err := metadata.ParseURL(r.Ref)
		if err != nil {
			t.Errorf("%s: resolver pipeline rejects ref %q: %v", r.Title, r.Ref, err)
			continue
		}
		if !provider.CanResolve(u) || !refPath.MatchString(u.Path) || !strings.HasPrefix(u.Path, "/"+string(r.MediaType)+"/"+r.ProviderID) {
			t.Errorf("%s: resolver would not take ref %q", r.Title, r.Ref)
		}
	}
}

func TestSearchFailuresMapToSharedErrors(t *testing.T) {
	for status, want := range map[int]error{429: metadata.ErrBusy, 503: metadata.ErrBusy, 401: metadata.ErrCredentials} {
		_, err := tmdb.New("token", searchClient(t, status, nil)).Search(context.Background(), metadata.SearchQuery{Text: "dune", Page: 1})
		if !errors.Is(err, want) {
			t.Errorf("status %d: err = %v, want %v", status, err, want)
		}
	}
	_, err := tmdb.New("", searchClient(t, 200, nil)).Search(context.Background(), metadata.SearchQuery{Text: "dune", Page: 1})
	if !errors.Is(err, metadata.ErrNotConfigured) {
		t.Errorf("empty token: %v", err)
	}
}
