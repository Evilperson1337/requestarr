package tmdb

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/providers/metadata"
)

const maxSearchResults = 50

// Search finds movies and TV shows by name with one multi-search call. People
// and other kinds are dropped. It reuses the resolver's client and token, and
// returns refs in the canonical form the resolver accepts.
func (r *Resolver) Search(ctx context.Context, q metadata.SearchQuery) (domain.SearchPage, error) {
	if r.token == "" {
		// Resolve would send no credentials and report them rejected; a search
		// has to say the provider is not set up.
		return domain.SearchPage{}, metadata.ErrNotConfigured
	}
	ctx, cancel := context.WithTimeout(ctx, 9*time.Second)
	defer cancel()
	page := max(q.Page, 1)
	params := url.Values{"query": {q.Text}, "page": {strconv.Itoa(page)}, "include_adult": {"false"}}
	var data struct {
		TotalPages int `json:"total_pages"`
		Results    []struct {
			MediaType string `json:"media_type"`
			ID        int64  `json:"id"`
			Title     string `json:"title"`
			Name      string `json:"name"`
			Release   string `json:"release_date"`
			FirstAir  string `json:"first_air_date"`
			Poster    string `json:"poster_path"`
		} `json:"results"`
	}
	h := http.Header{"Authorization": {"Bearer " + r.token}}
	if err := metadata.Fetch(ctx, r.client, "https://api.themoviedb.org/3/search/multi?"+params.Encode(), h, &data); err != nil {
		return domain.SearchPage{}, err
	}
	out := domain.SearchPage{Results: []domain.SearchResult{}, HasMore: page < data.TotalPages}
	for _, row := range data.Results {
		var typ domain.MediaType
		title, date := cleanText(row.Title, 256), row.Release
		switch row.MediaType {
		case "movie":
			typ = domain.MediaMovie
		case "tv":
			typ, title, date = domain.MediaTV, cleanText(row.Name, 256), row.FirstAir
		default:
			continue // people, collections and anything new TMDB adds
		}
		if row.ID <= 0 || title == "" || len(out.Results) >= maxSearchResults {
			continue
		}
		res := domain.SearchResult{
			MediaType: typ, Provider: "tmdb", ProviderID: strconv.FormatInt(row.ID, 10),
			Ref:   fmt.Sprintf("https://www.themoviedb.org/%s/%d", row.MediaType, row.ID),
			Title: title, ArtworkURL: imageURL(row.Poster, "w500"), ArtworkKind: domain.ArtworkPoster,
		}
		if _, err := time.Parse("2006-01-02", date); err == nil {
			res.Year, _ = strconv.Atoi(date[:4])
		}
		out.Results = append(out.Results, res)
	}
	return out, nil
}
