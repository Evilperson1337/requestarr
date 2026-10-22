package igdb

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/providers/metadata"
)

type Resolver struct {
	client          *http.Client
	clientID, token string
}

func New(clientID, token string, client *http.Client) *Resolver {
	if client == nil {
		client = &http.Client{Timeout: 9 * time.Second}
	}
	return &Resolver{client: client, clientID: clientID, token: token}
}
func (*Resolver) Name() string { return "igdb" }
func (*Resolver) CanResolve(u *url.URL) bool {
	if u == nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return (host == "igdb.com" || host == "www.igdb.com") && (u.Port() == "" || u.Port() == "443")
}
func (r *Resolver) Resolve(ctx context.Context, u *url.URL) (domain.ResolvedItem, error) {
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 || parts[0] != "games" || r.clientID == "" || r.token == "" {
		return domain.ResolvedItem{}, fmt.Errorf("unsupported IGDB URL or missing credentials")
	}
	id := parts[1]
	if len(id) > 32 {
		return domain.ResolvedItem{}, fmt.Errorf("invalid IGDB ID")
	}
	if _, err := strconv.ParseUint(id, 10, 64); err != nil {
		return domain.ResolvedItem{}, fmt.Errorf("invalid IGDB ID")
	}
	endpoint := "https://api.igdb.com/v4/games"
	h := http.Header{"Client-ID": {r.clientID}, "Authorization": {"Bearer " + r.token}, "Content-Type": {"text/plain"}}
	var rows []struct {
		ID           uint64 `json:"id"`
		Name         string `json:"name"`
		Summary      string `json:"summary"`
		FirstRelease int64  `json:"first_release_date"`
		Cover        struct {
			ImageID string `json:"image_id"`
		} `json:"cover"`
		Platforms []struct {
			ID   uint64 `json:"id"`
			Name string `json:"name"`
		} `json:"platforms"`
	}
	if err := metadata.FetchWithBody(ctx, r.client, endpoint, "fields name,summary,first_release_date,cover.image_id,platforms.id,platforms.name; where id = "+id+";", h, &rows); err != nil {
		return domain.ResolvedItem{}, err
	}
	if len(rows) != 1 || rows[0].Name == "" {
		return domain.ResolvedItem{}, fmt.Errorf("IGDB game not found")
	}
	g := rows[0]
	item := domain.CatalogItem{MediaType: domain.MediaGame, Provider: "igdb", ProviderID: strconv.FormatUint(g.ID, 10), Title: g.Name, ArtworkKind: domain.ArtworkPoster, ExternalURL: "https://www.igdb.com/games/" + url.PathEscape(id), Metadata: map[string]any{"summary": g.Summary}}
	if g.FirstRelease > 0 {
		item.Year = time.Unix(g.FirstRelease, 0).UTC().Year()
	}
	if g.Cover.ImageID != "" && len(g.Cover.ImageID) < 128 {
		item.ArtworkURL = "https://images.igdb.com/igdb/image/upload/t_cover_big/" + g.Cover.ImageID + ".jpg"
	}
	out := domain.ResolvedItem{Item: item}
	for _, p := range g.Platforms {
		if p.ID > 0 && p.Name != "" {
			out.Platforms = append(out.Platforms, domain.Platform{ProviderID: strconv.FormatUint(p.ID, 10), Key: "igdb:" + strconv.FormatUint(p.ID, 10), Name: p.Name})
		}
	}
	return out, nil
}
