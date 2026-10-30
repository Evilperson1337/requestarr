package tmdb

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/providers/metadata"
)

type Resolver struct {
	client *http.Client
	token  string
}

func New(token string, client *http.Client) *Resolver {
	if client == nil {
		client = &http.Client{Timeout: 9 * time.Second}
	}
	bounded := *client
	if bounded.Timeout <= 0 || bounded.Timeout > 9*time.Second {
		bounded.Timeout = 9 * time.Second
	}
	bounded.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Resolver{client: &bounded, token: token}
}
func (*Resolver) Name() string { return "tmdb" }
func (*Resolver) CanResolve(u *url.URL) bool {
	return u != nil && (u.Hostname() == "themoviedb.org" || strings.HasSuffix(u.Hostname(), ".themoviedb.org") || u.Hostname() == "tmdb.org" || strings.HasSuffix(u.Hostname(), ".tmdb.org"))
}
func (r *Resolver) Resolve(ctx context.Context, u *url.URL) (domain.ResolvedItem, error) {
	ctx, cancel := context.WithTimeout(ctx, 9*time.Second)
	defer cancel()
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 || (parts[0] != "movie" && parts[0] != "tv") {
		return domain.ResolvedItem{}, fmt.Errorf("unsupported TMDB URL")
	}
	// TMDB's readable URLs append a title after the numeric provider ID.
	id, suffix, hasSuffix := strings.Cut(parts[1], "-")
	if id == "" || len(id) > 12 || hasSuffix && suffix == "" {
		return domain.ResolvedItem{}, fmt.Errorf("invalid TMDB ID")
	}
	for _, r := range id {
		if r < '0' || r > '9' {
			return domain.ResolvedItem{}, fmt.Errorf("invalid TMDB ID")
		}
	}
	endpoint := "https://api.themoviedb.org/3/" + parts[0] + "/" + url.PathEscape(id)
	var data struct {
		ID             int64  `json:"id"`
		Title          string `json:"title"`
		Name           string `json:"name"`
		Date           string `json:"release_date"`
		FirstAir       string `json:"first_air_date"`
		LastAir        string `json:"last_air_date"`
		EpisodeRuntime []int  `json:"episode_run_time"`
		Seasons        int    `json:"number_of_seasons"`
		Episodes       int    `json:"number_of_episodes"`
		Creators       []struct {
			Name string `json:"name"`
		} `json:"created_by"`
		Poster   string `json:"poster_path"`
		Backdrop string `json:"backdrop_path"`
		Overview string `json:"overview"`
		Runtime  int    `json:"runtime"`
		Genres   []struct {
			Name string `json:"name"`
		} `json:"genres"`
		Status      string  `json:"status"`
		Score       float64 `json:"vote_average"`
		RatingCount int64   `json:"vote_count"`
	}
	h := http.Header{}
	if r.token != "" {
		h.Set("Authorization", "Bearer "+r.token)
	}
	if err := metadata.Fetch(ctx, r.client, endpoint, h, &data); err != nil {
		return domain.ResolvedItem{}, err
	}
	if data.ID <= 0 {
		return domain.ResolvedItem{}, fmt.Errorf("invalid TMDB response ID")
	}
	title, date, typ := data.Title, data.Date, domain.MediaMovie
	if parts[0] == "tv" {
		title, date, typ = data.Name, data.FirstAir, domain.MediaTV
	}
	if title == "" {
		return domain.ResolvedItem{}, fmt.Errorf("TMDB response has no title")
	}
	item := domain.CatalogItem{MediaType: typ, Provider: "tmdb", ProviderID: fmt.Sprint(data.ID), Title: title, ArtworkKind: domain.ArtworkPoster, ExternalURL: fmt.Sprintf("https://www.themoviedb.org/%s/%d", parts[0], data.ID), Metadata: map[string]any{"overview": data.Overview}}
	if len(date) >= 4 {
		fmt.Sscanf(date[:4], "%d", &item.Year)
	}
	if data.Poster != "" && strings.HasPrefix(data.Poster, "/") {
		item.ArtworkURL = "https://image.tmdb.org/t/p/w500" + data.Poster
	}
	resolved := domain.ResolvedItem{Item: item}
	if typ == domain.MediaMovie {
		movie := &domain.MovieDetails{Overview: cleanText(data.Overview, 10000)}
		if data.Runtime > 0 && data.Runtime <= 1440 {
			movie.Runtime = data.Runtime
		}
		for _, genre := range data.Genres {
			if name := cleanText(genre.Name, 80); name != "" && len(movie.Genres) < 20 {
				movie.Genres = append(movie.Genres, name)
			}
		}
		if _, err := time.Parse("2006-01-02", data.Date); err == nil {
			movie.ReleaseDate = data.Date
		} else {
			resolved.Item.Year = 0
		}
		switch data.Status {
		case "Rumored", "Planned", "In Production", "Post Production", "Released", "Canceled":
			movie.ReleaseStatus = data.Status
		}
		if data.RatingCount > 0 && data.RatingCount <= 1000000000 && data.Score >= 0 && data.Score <= 10 {
			movie.Score = &data.Score
			movie.RatingCount = data.RatingCount
		}
		movie.BackdropURL = imageURL(data.Backdrop, "w1280")
		r.credits(ctx, endpoint+"/credits", h, movie)
		if ctx.Err() != nil {
			return domain.ResolvedItem{}, ctx.Err()
		}
		resolved.Movie = movie
		resolved.Item.Metadata = map[string]any{"movie": movie}
		resolved.Item.ArtworkURL = imageURL(data.Poster, "w500")
	}
	if typ == domain.MediaTV {
		tv := &domain.TVDetails{Overview: cleanText(data.Overview, 10000), BackdropURL: imageURL(data.Backdrop, "w1280")}
		for _, runtime := range data.EpisodeRuntime {
			if runtime > 0 && runtime <= 1440 {
				tv.EpisodeRuntime = runtime
				break
			}
		}
		for _, genre := range data.Genres {
			if name := cleanText(genre.Name, 80); name != "" && len(tv.Genres) < 20 {
				tv.Genres = append(tv.Genres, name)
			}
		}
		if _, err := time.Parse("2006-01-02", data.FirstAir); err == nil {
			tv.FirstAirDate = data.FirstAir
		} else {
			resolved.Item.Year = 0
		}
		if _, err := time.Parse("2006-01-02", data.LastAir); err == nil {
			tv.LastAirDate = data.LastAir
		}
		switch data.Status {
		case "Returning Series", "Planned", "In Production", "Ended", "Canceled", "Pilot":
			tv.SeriesStatus = data.Status
		}
		if data.Seasons > 0 && data.Seasons <= 1000 {
			tv.Seasons = data.Seasons
		}
		if data.Episodes > 0 && data.Episodes <= 100000 {
			tv.Episodes = data.Episodes
		}
		if data.RatingCount > 0 && data.RatingCount <= 1000000000 && data.Score >= 0 && data.Score <= 10 {
			tv.Score = &data.Score
			tv.RatingCount = data.RatingCount
		}
		seen := map[string]bool{}
		for _, person := range data.Creators {
			if name := cleanText(person.Name, 256); name != "" && !seen[name] && len(tv.Creators) < 10 {
				tv.Creators = append(tv.Creators, name)
				seen[name] = true
			}
		}
		credits := &domain.MovieDetails{}
		r.credits(ctx, endpoint+"/credits", h, credits)
		if ctx.Err() != nil {
			return domain.ResolvedItem{}, ctx.Err()
		}
		tv.Cast, tv.DetailsUnavailable = credits.Cast, credits.DetailsUnavailable
		resolved.TV = tv
		resolved.Item.Metadata = map[string]any{"tv": tv}
		resolved.Item.ArtworkURL = imageURL(data.Poster, "w500")
	}
	return resolved, nil
}
