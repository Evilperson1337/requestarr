package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/evilperson1337/requestarr/internal/auth"
	"github.com/evilperson1337/requestarr/internal/buildinfo"
	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/providers/metadata"
	"github.com/evilperson1337/requestarr/internal/providers/metadata/tmdb"
)

type tmdbFixtureTransport struct {
	calls         int
	creditsStatus int
}

func (f *tmdbFixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Path == "/3/movie/1762506/credits" {
		status := f.creditsStatus
		if status == 0 {
			status = http.StatusOK
		}
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"crew":[{"job":"Director","name":"Fixture Director"}],"cast":[{"name":"Fixture Actor","character":"The lead"}]}`))}, nil
	}
	f.calls++
	if r.Method != http.MethodGet || r.URL.String() != "https://api.themoviedb.org/3/movie/1762506" {
		return nil, fmt.Errorf("unexpected fixture endpoint")
	}
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":1762506,"title":"Fixture movie","release_date":"2026-01-01","status":"Released","overview":"An official synopsis.","runtime":145,"genres":[{"id":12,"name":"Adventure"}],"vote_average":7.8,"vote_count":120}`))}, nil
}

func TestMoviePreviewCreditsAndPartialFailure(t *testing.T) {
	for _, status := range []int{200, 503} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			transport := &tmdbFixtureTransport{creditsStatus: status}
			h := NewWithMetadata(testLogger(), buildinfo.Info{}, fakeReadiness{}, metadata.NewService(&tmdbFixtureCatalog{}, tmdb.New("", &http.Client{Transport: transport})), &auth.Handler{Repo: csrfAuthRepo{statusAuthRepo{user: auth.User{ID: "user-1", Role: "requester"}}}, Secret: "test-secret", PublicURL: "http://example.test"})
			r := httptest.NewRequest(http.MethodPost, "/api/v1/resolve", strings.NewReader(`{"url":"https://www.themoviedb.org/movie/1762506-fixture"}`))
			r.AddCookie(&http.Cookie{Name: "requestarr_session", Value: "valid"})
			r.AddCookie(&http.Cookie{Name: "requestarr_csrf", Value: "csrf-token"})
			r.Header.Set("X-CSRF-Token", "csrf-token")
			r.Header.Set("Origin", "http://example.test")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, r)
			if rec.Code != 200 {
				t.Fatalf("core preview status=%d", rec.Code)
			}
			var result struct {
				Movie struct {
					Directors []string `json:"directors"`
					Cast      []struct {
						Name string `json:"name"`
					} `json:"cast"`
					Unavailable bool `json:"detailsUnavailable"`
				}
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if status == 200 && (len(result.Movie.Directors) != 1 || len(result.Movie.Cast) != 1) {
				t.Fatal("credits not exposed in movie preview")
			}
			if status != 200 && !result.Movie.Unavailable {
				t.Fatal("secondary failure did not preserve core with an honest notice")
			}
		})
	}
}

type tmdbFixtureCatalog struct{ items []domain.CatalogItem }

func (c *tmdbFixtureCatalog) Upsert(_ context.Context, resolved domain.ResolvedItem) (domain.CatalogItem, error) {
	item := resolved.Item
	for _, existing := range c.items {
		if existing.Provider == item.Provider && existing.ProviderID == item.ProviderID && existing.MediaType == item.MediaType {
			return existing, nil
		}
	}
	item.ID = fmt.Sprintf("22222222-2222-4222-8222-%012d", len(c.items)+1)
	c.items = append(c.items, item)
	return item, nil
}

func TestResolveAPIAcceptsTMDBTitleSuffixAndPreservesCanonicalCatalogID(t *testing.T) {
	for _, role := range []string{"viewer", "requester", "admin"} {
		t.Run(role, func(t *testing.T) {
			catalog := &tmdbFixtureCatalog{}
			transport := &tmdbFixtureTransport{}
			resolver := metadata.NewService(catalog, tmdb.New("token-canary", &http.Client{Transport: transport}))
			handler := NewWithMetadata(testLogger(), buildinfo.Info{}, fakeReadiness{}, resolver, &auth.Handler{
				Repo: csrfAuthRepo{statusAuthRepo{user: auth.User{ID: "user-1", Role: role}}}, Secret: "test-secret", PublicURL: "http://example.test",
			})
			for _, raw := range []string{
				"https://www.themoviedb.org/movie/1762506-band-of-brothers-legacy",
				"https://www.themoviedb.org/movie/1762506",
			} {
				body, err := json.Marshal(map[string]string{"url": raw})
				if err != nil {
					t.Fatal(err)
				}
				req := httptest.NewRequest(http.MethodPost, "/api/v1/resolve", strings.NewReader(string(body)))
				req.AddCookie(&http.Cookie{Name: "requestarr_session", Value: "valid"})
				req.AddCookie(&http.Cookie{Name: "requestarr_csrf", Value: "csrf-token"})
				req.Header.Set("X-CSRF-Token", "csrf-token")
				req.Header.Set("Origin", "http://example.test")
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)
				if rec.Code != http.StatusOK {
					t.Fatalf("resolve status=%d; want 200", rec.Code)
				}
				if strings.Contains(rec.Body.String(), "token-canary") {
					t.Fatal("provider credential leaked into the API response")
				}
				var resolved domain.ResolvedItem
				if err := json.Unmarshal(rec.Body.Bytes(), &resolved); err != nil {
					t.Fatal(err)
				}
				if resolved.Item.ID != "22222222-2222-4222-8222-000000000001" || resolved.Item.ProviderID != "1762506" || resolved.Item.MediaType != domain.MediaMovie || resolved.Item.ExternalURL != "https://www.themoviedb.org/movie/1762506" {
					t.Fatal("API did not return the expected canonical catalog item")
				}
				var preview struct {
					Movie struct {
						Runtime     int      `json:"runtime"`
						Overview    string   `json:"overview"`
						Score       float64  `json:"score"`
						RatingCount int      `json:"ratingCount"`
						Genres      []string `json:"genres"`
					}
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &preview); err != nil {
					t.Fatal(err)
				}
				if preview.Movie.Runtime != 145 || preview.Movie.Overview != "An official synopsis." || preview.Movie.Score != 7.8 || preview.Movie.RatingCount != 120 || len(preview.Movie.Genres) != 1 || preview.Movie.Genres[0] != "Adventure" {
					t.Fatal("API did not expose bounded typed movie details")
				}
				var actions struct {
					Preview *struct {
						Availability string `json:"availability"`
						CanRequest   bool   `json:"canRequest"`
					}
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &actions); err != nil {
					t.Fatal(err)
				}
				if actions.Preview == nil || actions.Preview.Availability != "unknown" || actions.Preview.CanRequest != (role != "viewer") {
					t.Fatal("preview did not expose role-appropriate local action state")
				}
			}
			if transport.calls != 2 || len(catalog.items) != 1 {
				t.Fatalf("provider requests=%d catalog items=%d; want 2 and 1", transport.calls, len(catalog.items))
			}
		})
	}
}
