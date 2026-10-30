package tmdb_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/providers/metadata"
	"github.com/evilperson1337/requestarr/internal/providers/metadata/tmdb"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestResolveMovieWithTitleSuffix(t *testing.T) {
	fixture, err := os.ReadFile("testdata/movie.json")
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/credits") {
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"cast":[],"crew":[]}`))}, nil
		}
		calls++
		if r.Method != http.MethodGet || r.URL.String() != "https://api.themoviedb.org/3/movie/1762506" {
			t.Errorf("unexpected metadata request: %s %s", r.Method, r.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(bytes.NewReader(fixture))}, nil
	})}
	u, err := url.Parse("https://www.themoviedb.org/movie/1762506-band-of-brothers-legacy")
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := tmdb.New("", client).Resolve(context.Background(), u)
	if err != nil {
		t.Fatal(err)
	}
	item := resolved.Item
	if calls != 1 || item.Provider != "tmdb" || item.ProviderID != "1762506" || item.MediaType != domain.MediaMovie || item.Title != "Fixture movie" || item.ExternalURL != "https://www.themoviedb.org/movie/1762506" {
		t.Fatalf("unexpected resolution: calls=%d item=%+v", calls, item)
	}
}

func TestNumericAndTitleSuffixedTMDBURLsResolveTheSameCatalogItem(t *testing.T) {
	for _, tc := range []struct {
		name, fixture, endpoint, numericURL, titleURL, id, title string
		mediaType                                                domain.MediaType
	}{
		{"movie", "movie.json", "https://api.themoviedb.org/3/movie/1762506", "https://www.themoviedb.org/movie/1762506", "https://www.themoviedb.org/movie/1762506-band-of-brothers-legacy", "1762506", "Fixture movie", domain.MediaMovie},
		{"TV", "tv.json", "https://api.themoviedb.org/3/tv/714", "https://www.themoviedb.org/tv/714", "https://www.themoviedb.org/tv/714-fixture-tv-series", "714", "Fixture TV series", domain.MediaTV},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture, err := os.ReadFile("testdata/" + tc.fixture)
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if strings.HasSuffix(r.URL.Path, "/credits") {
					return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"cast":[],"crew":[]}`))}, nil
				}
				calls++
				if r.URL.String() != tc.endpoint || r.Method != http.MethodGet {
					t.Errorf("unexpected metadata request: %s %s", r.Method, r.URL)
				}
				if r.Header.Get("Authorization") != "Bearer token-canary" {
					t.Error("missing expected provider authorization")
				}
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(bytes.NewReader(fixture))}, nil
			})}
			service := metadata.NewService(nil, tmdb.New("token-canary", client))
			numeric, err := service.Resolve(context.Background(), tc.numericURL)
			if err != nil {
				t.Fatal("numeric-only URL did not resolve")
			}
			withTitle, err := service.Resolve(context.Background(), tc.titleURL)
			if err != nil {
				t.Fatal("title-suffixed URL did not resolve")
			}
			if calls != 2 || !reflect.DeepEqual(numeric, withTitle) {
				t.Fatalf("URL forms did not resolve the same item: provider requests=%d", calls)
			}
			item := withTitle.Item
			if item.Provider != "tmdb" || item.ProviderID != tc.id || item.MediaType != tc.mediaType || item.Title != tc.title || item.ExternalURL != tc.numericURL {
				t.Fatal("resolved item did not preserve canonical identity and media type")
			}
			if strings.Contains(item.ExternalURL, "token-canary") {
				t.Fatal("credential leaked into the catalog URL")
			}
		})
	}
}

func TestRejectMalformedTMDBURLsBeforeProviderRequests(t *testing.T) {
	for _, raw := range []string{
		"https://www.themoviedb.org/movie/",
		"https://www.themoviedb.org/movie/-title",
		"https://www.themoviedb.org/movie/not-a-number",
		"https://www.themoviedb.org/movie/1762506x-title",
		"https://www.themoviedb.org/movie/1234567890123",
		"https://www.themoviedb.org/movie/1234567890123-title",
		"https://www.themoviedb.org/movie/1762506-",
		"https://www.themoviedb.org/tv/714x-title",
		"https://www.themoviedb.org/person/1762506-title",
		"https://evil.example/movie/1762506-title",
		"https://www.themoviedb.org.evil.example/movie/1762506-title",
	} {
		t.Run(raw, func(t *testing.T) {
			calls := 0
			client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: http.StatusNotFound, Header: http.Header{}, Body: http.NoBody}, nil
			})}
			service := metadata.NewService(nil, tmdb.New("", client))
			if _, err := service.Resolve(context.Background(), raw); err == nil {
				t.Fatal("malformed metadata URL was accepted")
			}
			if calls != 0 {
				t.Fatal("malformed metadata URL made a provider request")
			}
		})
	}
}
