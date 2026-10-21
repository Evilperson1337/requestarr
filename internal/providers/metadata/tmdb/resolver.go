package tmdb

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/example/requestarr/internal/domain"
	"github.com/example/requestarr/internal/providers/metadata"
)

type Resolver struct {
	client *http.Client
	token  string
}

func New(token string, client *http.Client) *Resolver {
	if client == nil {
		client = &http.Client{Timeout: 9 * time.Second}
	}
	return &Resolver{client: client, token: token}
}
func (*Resolver) Name() string { return "tmdb" }
func (*Resolver) CanResolve(u *url.URL) bool {
	return u != nil && (u.Hostname() == "themoviedb.org" || strings.HasSuffix(u.Hostname(), ".themoviedb.org") || u.Hostname() == "tmdb.org" || strings.HasSuffix(u.Hostname(), ".tmdb.org"))
}
func (r *Resolver) Resolve(ctx context.Context, u *url.URL) (domain.ResolvedItem, error) {
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 || (parts[0] != "movie" && parts[0] != "tv") {
		return domain.ResolvedItem{}, fmt.Errorf("unsupported TMDB URL")
	}
	id := parts[1]
	if id == "" || len(id) > 12 {
		return domain.ResolvedItem{}, fmt.Errorf("invalid TMDB ID")
	}
	for _, r := range id {
		if r < '0' || r > '9' {
			return domain.ResolvedItem{}, fmt.Errorf("invalid TMDB ID")
		}
	}
	endpoint := "https://api.themoviedb.org/3/" + parts[0] + "/" + url.PathEscape(id)
	var data struct {
		ID       int64  `json:"id"`
		Title    string `json:"title"`
		Name     string `json:"name"`
		Date     string `json:"release_date"`
		FirstAir string `json:"first_air_date"`
		Poster   string `json:"poster_path"`
		Overview string `json:"overview"`
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
	return domain.ResolvedItem{Item: item}, nil
}
