package audible

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/providers/metadata"
)

const asin = "B08G9PRS1K"

const nexusBody = `{"asin":"B08G9PRS1K","title":"Project Hail Mary","subtitle":"A Novel","authors":[{"name":"Andy Weir"}],"narrators":[{"name":"Ray Porter"}],
"image":"https://m.media-amazon.com/images/I/91vS2L5YfEL._SL500_.jpg","formatType":"unabridged","language":"english","releaseDate":"2021-05-04T00:00:00.000Z","runtimeLengthMin":970,
"seriesPrimary":{"name":"Hail Mary","position":"1"},"region":"us"}`

const catalogBody = `{"product":{"asin":"B08G9PRS1K","title":"Project Hail Mary","authors":[{"name":"Andy Weir"}],"narrators":[{"name":"Ray Porter"}],
"product_images":{"500":"https://m.media-amazon.com/images/I/91vS2L5YfEL._SL500_.jpg"},"format_type":"unabridged","language":"english","release_date":"2021-05-04","runtime_length_min":970,
"series":[{"title":"Hail Mary","sequence":"1"}]}}`

// fakeUpstreams serves Audnexus and the Audible catalog from one test server.
func fakeUpstreams(t *testing.T, nexus, catalog func(w http.ResponseWriter, r *http.Request)) (*Resolver, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Path, "/books/") {
			nexus(w, r)
		} else {
			catalog(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	r := New(domain.RegionUS, srv.Client())
	r.audnexus = srv.URL
	r.catalog = func(domain.AudibleRegion) string { return srv.URL }
	return r, &calls
}

func body(s string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(s)) }
}

func status(code int) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(code) }
}

func resolve(t *testing.T, r *Resolver, raw string) (domain.ResolvedItem, error) {
	t.Helper()
	canonical, ok := r.Canonicalize(raw)
	if !ok {
		t.Fatalf("%q not canonicalized", raw)
	}
	u, _ := url.Parse(canonical)
	return r.Resolve(context.Background(), u)
}

func TestResolvesFromAudnexus(t *testing.T) {
	r, calls := fakeUpstreams(t, body(nexusBody), status(500))
	got, err := resolve(t, r, " b08g9prs1k ")
	if err != nil {
		t.Fatal(err)
	}
	item := got.Item
	if item.MediaType != domain.MediaAudiobook || item.Provider != "audible" || item.ProviderID != asin || item.Title != "Project Hail Mary" || item.Year != 2021 ||
		item.ArtworkKind != domain.ArtworkSquare || !strings.HasPrefix(item.ArtworkURL, "https://m.media-amazon.com/") || item.ExternalURL != "https://www.audible.com/pd/"+asin {
		t.Fatalf("unexpected item %+v", item)
	}
	if item.Unresolved() || item.Metadata["abridged"] != false || item.Metadata["region"] != "us" || item.Metadata["series"] != "Hail Mary" {
		t.Fatalf("unexpected metadata %v", item.Metadata)
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("%d upstream calls, want 1", n)
	}
}

func TestFallsBackToAudibleCatalogWhenAudnexusFails(t *testing.T) {
	r, _ := fakeUpstreams(t, status(404), body(catalogBody))
	got, err := resolve(t, r, asin)
	if err != nil || got.Item.Title != "Project Hail Mary" || got.Item.Unresolved() {
		t.Fatalf("got %+v, %v", got.Item, err)
	}
}

func TestRegionFromURLOverridesTheDefault(t *testing.T) {
	var region string
	r, _ := fakeUpstreams(t, func(w http.ResponseWriter, r *http.Request) {
		region = r.URL.Query().Get("region")
		_, _ = w.Write([]byte(nexusBody))
	}, status(500))
	got, err := resolve(t, r, "https://www.audible.co.uk/pd/Slug/"+asin+"?qid=1&source_code=X")
	if err != nil || region != "uk" || got.Item.Metadata["region"] != "uk" || got.Item.ExternalURL != "https://www.audible.co.uk/pd/"+asin {
		t.Fatalf("region %q, item %+v, err %v", region, got.Item, err)
	}
}

func TestRegionUnavailableEverywhereIsNotFound(t *testing.T) {
	r, _ := fakeUpstreams(t, status(404), status(404))
	_, err := resolve(t, r, asin)
	if !errors.Is(err, metadata.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestBothUpstreamsDownAcceptsThePlaceholder(t *testing.T) {
	for name, nexus := range map[string]func(http.ResponseWriter, *http.Request){
		"500": status(500), "malformed": body(`{"title":`), "no title": body(`{"title":" "}`),
		"html": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<html>"))
		},
	} {
		t.Run(name, func(t *testing.T) {
			r, _ := fakeUpstreams(t, nexus, status(500))
			got, err := resolve(t, r, asin)
			if err != nil {
				t.Fatal(err)
			}
			item := got.Item
			if !item.Unresolved() || item.ProviderID != asin || item.Title != "Audiobook "+asin || item.Provider != "audible" {
				t.Fatalf("unexpected placeholder %+v", item)
			}
		})
	}
}

func TestOversizedResponseIsNotTrusted(t *testing.T) {
	huge := `{"title":"` + strings.Repeat("a", 3<<20) + `"}`
	r, _ := fakeUpstreams(t, body(huge), status(500))
	got, err := resolve(t, r, asin)
	if err != nil || !got.Item.Unresolved() {
		t.Fatalf("got %+v, %v", got.Item, err)
	}
}

func TestCancelledContextIsAnErrorNotAPlaceholder(t *testing.T) {
	r, _ := fakeUpstreams(t, status(500), status(500))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	u, _ := url.Parse("https://www.audible.com/pd/" + asin)
	if _, err := r.Resolve(ctx, u); err == nil {
		t.Fatal("cancelled resolve succeeded")
	}
}

func TestCatalogProductForAnotherASINIsRejected(t *testing.T) {
	r, _ := fakeUpstreams(t, status(404), body(strings.Replace(catalogBody, `"asin":"B08G9PRS1K"`, `"asin":"B000000000"`, 1)))
	got, _ := resolve(t, r, asin)
	if !got.Item.Unresolved() {
		t.Fatalf("accepted a product for a different ASIN: %+v", got.Item)
	}
}

func TestCoverMustBeAnAmazonImage(t *testing.T) {
	r, _ := fakeUpstreams(t, body(strings.Replace(nexusBody, "https://m.media-amazon.com/", "https://evil.example/", 1)), status(500))
	got, err := resolve(t, r, asin)
	if err != nil || got.Item.ArtworkURL != "" {
		t.Fatalf("cover %q, err %v", got.Item.ArtworkURL, err)
	}
}

func TestCanonicalize(t *testing.T) {
	r := New(domain.RegionDE, nil)
	for in, want := range map[string]string{
		asin: "https://www.audible.de/pd/" + asin, "https://audible.com/pd/Slug/" + asin + "/?x=1#y": "https://www.audible.com/pd/" + asin,
	} {
		if got, ok := r.Canonicalize(in); !ok || got != want {
			t.Errorf("Canonicalize(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	for _, in := range []string{"", "https://www.amazon.com/dp/" + asin, "https://www.themoviedb.org/movie/603", "B08G9PRS1", "not a url"} {
		if got, ok := r.Canonicalize(in); ok {
			t.Errorf("Canonicalize(%q) = %q; want no", in, got)
		}
	}
	if New("zz", nil).defaultRegion != domain.RegionUS {
		t.Error("unknown default region not replaced by US")
	}
}

func TestServiceResolvesABareASINAndDoesNotCachePlaceholders(t *testing.T) {
	r, calls := fakeUpstreams(t, status(500), status(500))
	svc := metadata.NewService(nil, r)
	for i := 0; i < 2; i++ {
		got, err := svc.Resolve(context.Background(), asin)
		if err != nil || !got.Item.Unresolved() {
			t.Fatalf("got %+v, %v", got.Item, err)
		}
	}
	first := calls.Load()
	if first < 4 {
		t.Fatalf("placeholder was cached: %d upstream calls for two resolves", first)
	}
}
