package audible

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/providers/metadata"
)

// searcher serves the recorded catalog search answer (testdata/search.json,
// trimmed from a live call) and records what was asked.
func searcher(t *testing.T, region domain.AudibleRegion, serve func(w http.ResponseWriter, r *http.Request)) (*Resolver, *[]*http.Request) {
	t.Helper()
	var seen []*http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r)
		w.Header().Set("Content-Type", "application/json")
		serve(w, r)
	}))
	t.Cleanup(srv.Close)
	r := New(region, srv.Client())
	r.catalog = func(domain.AudibleRegion) string { return srv.URL }
	return r, &seen
}

func fixture(t *testing.T) func(http.ResponseWriter, *http.Request) {
	t.Helper()
	data, err := os.ReadFile("testdata/search.json")
	if err != nil {
		t.Fatal(err)
	}
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
	}
}

func TestSearchMapsCatalogProductsToRows(t *testing.T) {
	r, seen := searcher(t, domain.RegionUS, fixture(t))
	page, err := r.Search(context.Background(), metadata.SearchQuery{Text: "hail mary", Page: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(*seen) != 1 {
		t.Fatalf("calls = %d, want one", len(*seen))
	}
	q := (*seen)[0].URL.Query()
	if (*seen)[0].URL.Path != "/1.0/catalog/products/" || q.Get("keywords") != "hail mary" || q.Get("page") != "1" || q.Get("num_results") != "25" {
		t.Fatalf("unexpected request %s", (*seen)[0].URL)
	}
	if !page.HasMore {
		t.Fatal("5 of 124 leaves more")
	}
	if len(page.Results) != 5 {
		t.Fatalf("rows = %d", len(page.Results))
	}
	first := page.Results[0]
	if first.MediaType != domain.MediaAudiobook || first.Provider != "audible" || first.ProviderID != "B08G9PRS1K" ||
		first.Ref != "https://www.audible.com/pd/B08G9PRS1K" || first.Title != "Project Hail Mary" || first.Year != 2021 ||
		first.ArtworkKind != domain.ArtworkSquare || !strings.HasPrefix(first.ArtworkURL, "https://m.media-amazon.com/") {
		t.Fatalf("first row %+v", first)
	}
	want := "Andy Weir · Narrated by Ray Porter · 16h 10m · Unabridged · Series / multi-part"
	if first.Subtitle != want {
		t.Fatalf("subtitle %q, want %q", first.Subtitle, want)
	}
	// A single-part book in a series carries no multi-part label.
	if got := page.Results[3].Subtitle; got != "Aer-ki Jyr · Narrated by Stephen Day · 11h 16m · Unabridged" {
		t.Fatalf("fourth row subtitle %q", got)
	}
}

// Every ref a search returns must be one the resolver's own canonicalization
// accepts and leaves unchanged, so picking a row previews exactly that item.
func TestSearchRefsAreCanonical(t *testing.T) {
	r, _ := searcher(t, domain.RegionUS, fixture(t))
	page, err := r.Search(context.Background(), metadata.SearchQuery{Text: "hail mary", Page: 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, res := range page.Results {
		got, ok := r.Canonicalize(res.Ref)
		if !ok || got != res.Ref {
			t.Errorf("%s: canonicalize(%q) = %q, %v", res.ProviderID, res.Ref, got, ok)
		}
		u, err := url.Parse(res.Ref)
		if err != nil || !r.CanResolve(u) {
			t.Errorf("%s: resolver refuses %q", res.ProviderID, res.Ref)
		}
	}
}

func TestSearchUsesTheAdminsMarketplace(t *testing.T) {
	r, _ := searcher(t, domain.RegionUK, fixture(t))
	inner := r.catalog
	var asked domain.AudibleRegion
	r.catalog = func(region domain.AudibleRegion) string { asked = region; return inner(region) }
	page, err := r.Search(context.Background(), metadata.SearchQuery{Text: "hail mary", Page: 1})
	if err != nil {
		t.Fatal(err)
	}
	if asked != domain.RegionUK || !strings.HasPrefix(page.Results[0].Ref, "https://www.audible.co.uk/pd/") {
		t.Fatalf("asked %q, ref %q", asked, page.Results[0].Ref)
	}
}

// A failed or unreadable answer is an error, never an empty page, and
// podcasts are not audiobooks.
func TestSearchFailuresAreErrorsNotEmptyPages(t *testing.T) {
	cases := map[string]func(http.ResponseWriter, *http.Request){
		"server error": func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) },
		"bad request":  func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(400) },
		"not json": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("<html>"))
		},
		"no products": func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"total_results":0}`)) },
		"wrong shape": func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"products":"nope"}`)) },
	}
	for name, serve := range cases {
		t.Run(name, func(t *testing.T) {
			r, _ := searcher(t, domain.RegionUS, serve)
			if page, err := r.Search(context.Background(), metadata.SearchQuery{Text: "hail mary", Page: 1}); err == nil {
				t.Fatalf("got %+v with no error", page)
			}
		})
	}
}

func TestSearchTrulyEmptyAnswerIsAnEmptyPage(t *testing.T) {
	r, _ := searcher(t, domain.RegionUS, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"products":[],"total_results":0}`))
	})
	page, err := r.Search(context.Background(), metadata.SearchQuery{Text: "zzqxwv", Page: 1})
	if err != nil || page.Results == nil || len(page.Results) != 0 || page.HasMore {
		t.Fatalf("page %+v err %v", page, err)
	}
}

func TestSearchSkipsPodcastsAndBadAsins(t *testing.T) {
	r, _ := searcher(t, domain.RegionUS, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"total_results":3,"products":[
{"asin":"B0B2N7XGVN","title":"A Podcast","content_type":"Podcast","content_delivery_type":"PodcastParent"},
{"asin":"nope","title":"Bad","content_type":"Product"},
{"asin":"B08G9PRS1K","title":"Good","content_type":"Product","content_delivery_type":"SinglePartBook","format_type":"abridged"}]}`))
	})
	page, err := r.Search(context.Background(), metadata.SearchQuery{Text: "good", Page: 1})
	if err != nil || len(page.Results) != 1 || page.Results[0].Title != "Good" || page.Results[0].Subtitle != "Abridged" {
		t.Fatalf("page %+v err %v", page, err)
	}
}
