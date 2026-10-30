package tmdb_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/providers/metadata/tmdb"
)

func TestTVOptionalCreditsAndValidationPreserveSeriesIdentity(t *testing.T) {
	fixture, err := os.ReadFile("testdata/tv.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		status  int
		invalid bool
	}{{"success", 200, false}, {"credits unavailable", 503, false}, {"invalid optional facts", 200, true}} {
		t.Run(tc.name, func(t *testing.T) {
			body := fixture
			if tc.invalid {
				var data map[string]any
				if json.Unmarshal(body, &data) != nil {
					t.Fatal("invalid test fixture")
				}
				data["number_of_seasons"] = 1001
				data["number_of_episodes"] = 100001
				data["episode_run_time"] = []int{-1, 0, 1500}
				data["status"] = "invented-status"
				data["first_air_date"] = "invalid-date"
				data["last_air_date"] = "invalid-date"
				data["vote_average"] = 99
				data["backdrop_path"] = "//evil.example/secret"
				body, err = json.Marshal(data)
				if err != nil {
					t.Fatal(err)
				}
			}
			calls := 0
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if strings.HasSuffix(r.URL.Path, "/credits") {
					return &http.Response{StatusCode: tc.status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"cast":[{"name":"Fixture Actor","character":"The lead"}],"crew":[{"job":"Director","name":"Episode Director"}]}`))}, nil
				}
				if r.URL.String() != "https://api.themoviedb.org/3/tv/714" {
					t.Error("TV request did not use the canonical series endpoint")
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(bytes.NewReader(body))}, nil
			})}
			u, _ := url.Parse("https://www.themoviedb.org/tv/714-fixture-series")
			result, err := tmdb.New("private-tv-token-canary", client).Resolve(context.Background(), u)
			if err != nil || result.TV == nil || result.Movie != nil || result.Item.MediaType != domain.MediaTV || result.Item.ProviderID != "714" || calls != 2 {
				t.Fatal("TV enrichment lost the canonical series preview")
			}
			if result.TV.DetailsUnavailable != (tc.status != 200) || len(result.TV.Creators) != 1 || result.TV.Creators[0] != "Fixture Creator" {
				t.Fatal("series creator or partial-failure state was incorrect")
			}
			if tc.invalid && (result.TV.Seasons != 0 || result.TV.Episodes != 0 || result.TV.EpisodeRuntime != 0 || result.TV.SeriesStatus != "" || result.TV.FirstAirDate != "" || result.TV.LastAirDate != "" || result.TV.Score != nil || result.TV.BackdropURL != "" || result.Item.Year != 0) {
				t.Fatal("invalid TV facts escaped validation")
			}
			encoded, _ := json.Marshal(result)
			if strings.Contains(string(encoded), "private-tv-token-canary") || strings.Contains(string(encoded), "Episode Director") || strings.Contains(string(encoded), "invented-status") {
				t.Fatal("credential or unapproved TV fields escaped")
			}
		})
	}
}
