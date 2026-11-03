package httpapi

import (
	"encoding/json"
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

type tvFixtureTransport struct {
	calls         int
	creditsStatus int
}

func (f *tvFixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	f.calls++
	status, body := 200, `{"id":714,"name":"Fixture series","first_air_date":"2020-01-02","last_air_date":"2025-03-04","status":"Returning Series","overview":"The official series synopsis.","episode_run_time":[45],"number_of_seasons":3,"number_of_episodes":30,"genres":[{"name":"Drama"}],"vote_average":8.2,"vote_count":400,"created_by":[{"name":"Fixture Creator"}]}`
	if strings.HasSuffix(r.URL.Path, "/credits") {
		body = `{"cast":[{"name":"Fixture Actor","character":"The lead"}],"crew":[{"job":"Director","name":"Episode Director"}]}`
		if f.creditsStatus != 0 {
			status = f.creditsStatus
		}
	} else if r.URL.String() != "https://api.themoviedb.org/3/tv/714" {
		return nil, io.ErrUnexpectedEOF
	}
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
}

func TestTVPreviewUsesSeriesDetailsAndWholeSeriesIdentity(t *testing.T) {
	for _, role := range []string{"viewer", "requester", "admin"} {
		t.Run(role, func(t *testing.T) {
			transport := &tvFixtureTransport{}
			catalog := &tmdbFixtureCatalog{}
			resolver := metadata.NewService(catalog, tmdb.New("token-canary", &http.Client{Transport: transport}))
			h := NewWithMetadata(testLogger(), buildinfo.Info{}, fakeReadiness{}, resolver, &auth.Handler{Repo: csrfAuthRepo{statusAuthRepo{user: auth.User{ID: "user-1", Role: role}}}, Secret: "test-secret", PublicURL: "http://example.test"})
			for _, raw := range []string{"https://www.themoviedb.org/tv/714-fixture-series", "https://www.themoviedb.org/tv/714"} {
				request := httptest.NewRequest("POST", "/api/v1/resolve", strings.NewReader(`{"url":"`+raw+`"}`))
				request.AddCookie(&http.Cookie{Name: "requestarr_session", Value: "valid"})
				request.AddCookie(&http.Cookie{Name: "requestarr_csrf", Value: "csrf-token"})
				request.Header.Set("X-CSRF-Token", "csrf-token")
				request.Header.Set("Origin", "http://example.test")
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, request)
				if rec.Code != 200 {
					t.Fatalf("TV preview status=%d", rec.Code)
				}
				var result struct {
					Item  domain.CatalogItem
					Movie json.RawMessage
					TV    struct {
						EpisodeRuntime int                `json:"episodeRuntime"`
						Seasons        int                `json:"seasons"`
						Episodes       int                `json:"episodes"`
						FirstAirDate   string             `json:"firstAirDate"`
						LastAirDate    string             `json:"lastAirDate"`
						SeriesStatus   string             `json:"seriesStatus"`
						Creators       []string           `json:"creators"`
						Cast           []domain.MovieCast `json:"cast"`
					}
					Preview *domain.MoviePreview
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				if result.Item.MediaType != domain.MediaTV || result.Item.ProviderID != "714" || result.Item.ExternalURL != "https://www.themoviedb.org/tv/714" || result.TV.EpisodeRuntime != 45 || result.TV.Seasons != 3 || result.TV.Episodes != 30 || result.TV.FirstAirDate != "2020-01-02" || result.TV.LastAirDate != "2025-03-04" || result.TV.SeriesStatus != "Returning Series" || len(result.TV.Creators) != 1 || result.TV.Creators[0] != "Fixture Creator" || len(result.TV.Cast) != 1 || result.Preview == nil || result.Preview.CanRequest != (role != "viewer") {
					t.Fatal("TV preview did not expose typed series details and local action state")
				}
				if len(result.Movie) != 0 || strings.Contains(rec.Body.String(), "Episode Director") || strings.Contains(rec.Body.String(), "token-canary") {
					t.Fatal("movie-only data or credentials escaped into TV preview")
				}
			}
			if len(catalog.items) != 1 || transport.calls != 4 {
				t.Fatal("TV URL variants lost canonical identity or made unexpected provider calls")
			}
		})
	}
}
