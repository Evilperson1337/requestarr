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

// The season list comes from the series response; episodes come from one appended request and are optional.
func TestTVSeasonsAndEpisodesFeedRequestSelection(t *testing.T) {
	for _, appended := range []bool{true, false} {
		client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			body := `{"cast":[],"crew":[]}`
			status := 200
			switch {
			case strings.HasSuffix(r.URL.Path, "/credits"):
			case r.URL.Query().Get("append_to_response") != "":
				if !appended {
					status = 503
				}
				body = `{"id":714,"season/1":{"episodes":[{"episode_number":1,"name":"Pilot","air_date":"2021-10-01"},{"episode_number":2,"name":"Second","air_date":"nope"}]},"season/0":{"episodes":[{"episode_number":1,"name":"Special"}]}}`
			default:
				body = `{"id":714,"name":"Fixture series","first_air_date":"2021-10-01","number_of_seasons":2,"seasons":[{"season_number":0,"name":"Specials","episode_count":1},{"season_number":1,"name":"Season 1","episode_count":2,"air_date":"2021-10-01"},{"season_number":2,"name":"Season 2","episode_count":0},{"season_number":-4,"name":"bad"}]}`
			}
			return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
		})}
		u, _ := url.Parse("https://www.themoviedb.org/tv/714-fixture-series")
		result, err := tmdb.New("token", client).Resolve(context.Background(), u)
		if err != nil || result.TV == nil {
			t.Fatalf("resolve: %v", err)
		}
		seasons := result.TV.SeasonList
		if len(seasons) != 3 || seasons[0].Number != 0 || seasons[1].Name != "Season 1" || seasons[1].EpisodeCount != 2 || seasons[2].EpisodeCount != 0 {
			t.Fatalf("seasons = %+v", seasons)
		}
		if appended {
			if len(seasons[1].Episodes) != 2 || seasons[1].Episodes[0].Name != "Pilot" || seasons[1].Episodes[1].AirDate != "" || len(seasons[0].Episodes) != 1 || len(seasons[2].Episodes) != 0 {
				t.Fatalf("episodes = %+v", seasons)
			}
		} else if len(seasons[1].Episodes) != 0 || result.TV.DetailsUnavailable {
			t.Fatalf("a failed episode lookup must leave seasons whole and the series available: %+v", result.TV)
		}
	}
}
