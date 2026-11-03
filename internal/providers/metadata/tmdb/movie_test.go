package tmdb_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/providers/metadata/tmdb"
)

func TestMovieEnrichmentIsBoundedAndOptional(t *testing.T) {
	for _, tc := range []struct {
		name, credits string
		status        int
		unavailable   bool
	}{
		{"valid", `{"crew":[{"job":"Director","name":"Director"},{"job":"Director","name":"Director"}],"cast":[{"name":"One","character":"Lead"},{"name":"Two"},{"name":"Three"},{"name":"Four"},{"name":"Five"},{"name":"Six"}]}`, 200, false},
		{"missing", `{"crew":[],"cast":[]}`, 200, false},
		{"malformed", `{`, 200, true},
		{"null", `null`, 200, true},
		{"timeout", `{}`, 200, true},
		{"wrong-content-type", `{}`, 200, true},
		{"non-success", "private-token-canary", 503, true},
		{"oversized", strings.Repeat("x", (2<<20)+1), 200, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				status, body := 200, `{"id":1762506,"title":"Fixture","release_date":"2026-01-01","runtime":-5,"vote_average":99,"vote_count":-1,"poster_path":"/../private","backdrop_path":"//evil.example/private","genres":[{"name":"Adventure"}]}`
				if strings.HasSuffix(r.URL.Path, "/credits") {
					deadline, ok := r.Context().Deadline()
					if !ok || time.Until(deadline) > 1600*time.Millisecond {
						t.Error("optional credits request had no short deadline")
					}
					status, body = tc.status, tc.credits
					if tc.name == "timeout" {
						<-r.Context().Done()
						return nil, r.Context().Err()
					}
					if tc.name == "wrong-content-type" {
						return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
					}
				}
				return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), ContentLength: int64(len(body))}, nil
			})}
			u, _ := url.Parse("https://www.themoviedb.org/movie/1762506-fixture")
			result, err := tmdb.New("private-token-canary", client).Resolve(context.Background(), u)
			if err != nil || result.Movie == nil || result.Movie.DetailsUnavailable != tc.unavailable || calls != 2 {
				t.Fatalf("optional enrichment failed core preview: error=%v calls=%d", err, calls)
			}
			if result.Movie.Runtime != 0 || result.Movie.Score != nil || result.Movie.BackdropURL != "" || result.Item.ArtworkURL != "" || len(result.Movie.Cast) > 5 {
				t.Fatal("untrusted optional facts escaped validation")
			}
			if tc.name == "valid" && (len(result.Movie.Cast) != 5 || len(result.Movie.Directors) != 1) {
				t.Fatal("credits were not deduplicated and limited")
			}
			body, _ := json.Marshal(result)
			if strings.Contains(string(body), "private-token-canary") {
				t.Fatal("provider credential or raw error escaped")
			}
		})
	}
}

func TestMovieResolverDoesNotFollowRedirectsOrMutateInjectedClient(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": {"https://evil.example/"}}, Body: http.NoBody}, nil
	})}
	u, _ := url.Parse("https://www.themoviedb.org/movie/1762506-fixture")
	_, err := tmdb.New("private-token-canary", client).Resolve(context.Background(), u)
	if err == nil {
		t.Fatal("redirect accepted")
	}
	if client.Timeout != 0 || client.CheckRedirect != nil {
		t.Fatal("constructor mutated injected client")
	}
}

func TestNearDeadlineCreditsDoNotDiscardResolvedCoreMovie(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	calls := 0
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if strings.HasSuffix(r.URL.Path, "/credits") {
			<-r.Context().Done()
			return nil, r.Context().Err()
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":1762506,"title":"Fixture"}`))}, nil
	})}
	u, _ := url.Parse("https://www.themoviedb.org/movie/1762506-fixture")
	result, err := tmdb.New("", client).Resolve(ctx, u)
	if err != nil || result.Movie == nil || !result.Movie.DetailsUnavailable || calls != 1 {
		t.Fatal("optional credits consumed the remaining core lookup budget")
	}
}

func TestCastPhotosAreApprovedTMDBImagesOnly(t *testing.T) {
	credits := `{"crew":[],"cast":[{"name":"Pictured","profile_path":"/abc123.jpg"},{"name":"Hostile","profile_path":"//evil.example/x.jpg"},{"name":"Traversal","profile_path":"/../private"},{"name":"None","profile_path":null}]}`
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body := `{"id":1762506,"title":"Fixture","release_date":"2026-01-01"}`
		if strings.HasSuffix(r.URL.Path, "/credits") {
			body = credits
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), ContentLength: int64(len(body))}, nil
	})}
	u, _ := url.Parse("https://www.themoviedb.org/movie/1762506-fixture")
	result, err := tmdb.New("private-token-canary", client).Resolve(context.Background(), u)
	if err != nil || result.Movie == nil || len(result.Movie.Cast) != 4 {
		t.Fatalf("cast lost: error=%v", err)
	}
	got := []string{}
	for _, person := range result.Movie.Cast {
		got = append(got, person.ProfileURL)
	}
	want := []string{"https://image.tmdb.org/t/p/w185/abc123.jpg", "", "", ""}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("profile URLs = %q, want %q", got, want)
	}
}
