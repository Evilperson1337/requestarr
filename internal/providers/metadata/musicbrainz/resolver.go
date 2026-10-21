package musicbrainz

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/example/requestarr/internal/domain"
	"github.com/example/requestarr/internal/providers/metadata"
)

type Resolver struct{ client *http.Client }

var mbidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func New(client *http.Client) *Resolver {
	if client == nil {
		client = &http.Client{Timeout: 9 * time.Second}
	}
	return &Resolver{client: client}
}
func (*Resolver) Name() string { return "musicbrainz" }
func (*Resolver) CanResolve(u *url.URL) bool {
	return u != nil && strings.EqualFold(u.Hostname(), "musicbrainz.org") && (u.Port() == "" || u.Port() == "443")
}
func (r *Resolver) Resolve(ctx context.Context, u *url.URL) (domain.ResolvedItem, error) {
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 {
		return domain.ResolvedItem{}, fmt.Errorf("unsupported MusicBrainz URL")
	}
	var entity string
	switch parts[0] {
	case "release":
		entity = "release"
	case "recording":
		entity = "recording"
	default:
		return domain.ResolvedItem{}, fmt.Errorf("unsupported MusicBrainz URL")
	}
	id := parts[1]
	if !mbidPattern.MatchString(id) {
		return domain.ResolvedItem{}, fmt.Errorf("invalid MusicBrainz identifier")
	}
	parsed, err := url.Parse("https://musicbrainz.org/ws/2/" + entity + "/" + url.PathEscape(id) + "?fmt=json&inc=artists")
	if err != nil {
		return domain.ResolvedItem{}, err
	}
	var data struct {
		ID           string `json:"id"`
		Title        string `json:"title"`
		Date         string `json:"date"`
		ArtistCredit []struct {
			Name   string `json:"name"`
			Artist struct {
				Name string `json:"name"`
			} `json:"artist"`
		} `json:"artist-credit"`
	}
	if err := metadata.Fetch(ctx, r.client, parsed.String(), nil, &data); err != nil {
		return domain.ResolvedItem{}, err
	}
	if data.ID == "" || data.Title == "" {
		return domain.ResolvedItem{}, fmt.Errorf("invalid MusicBrainz response")
	}
	title := data.Title
	if len(data.ArtistCredit) > 0 {
		name := data.ArtistCredit[0].Name
		if name == "" {
			name = data.ArtistCredit[0].Artist.Name
		}
		if name != "" {
			title = name + " — " + title
		}
	}
	item := domain.CatalogItem{MediaType: domain.MediaAlbum, Provider: "musicbrainz", ProviderID: data.ID, Title: title, ArtworkKind: domain.ArtworkSquare, ExternalURL: "https://musicbrainz.org/" + entity + "/" + data.ID}
	if len(data.Date) >= 4 {
		fmt.Sscanf(data.Date[:4], "%d", &item.Year)
	}
	return domain.ResolvedItem{Item: item}, nil
}
