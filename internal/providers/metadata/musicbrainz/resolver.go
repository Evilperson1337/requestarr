package musicbrainz

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/providers/metadata"
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
	// An album's identity is its release-group and a song's is its recording.
	// A pasted release link is accepted but resolved to its release-group, since
	// a release is only one edition of the album (never equate the two).
	var entity, inc string
	media := domain.MediaAlbum
	switch parts[0] {
	case "release-group":
		entity, inc = "release-group", "artists"
	case "recording":
		entity, inc, media = "recording", "artists", domain.MediaSong
	case "release":
		entity, inc = "release", "artists+release-groups"
	default:
		return domain.ResolvedItem{}, fmt.Errorf("unsupported MusicBrainz URL")
	}
	id := parts[1]
	if !mbidPattern.MatchString(id) {
		return domain.ResolvedItem{}, fmt.Errorf("invalid MusicBrainz identifier")
	}
	endpoint := "https://musicbrainz.org/ws/2/" + entity + "/" + url.PathEscape(id) + "?fmt=json&inc=" + inc
	type credit struct {
		Name   string `json:"name"`
		Artist struct {
			Name string `json:"name"`
		} `json:"artist"`
	}
	var data struct {
		ID               string   `json:"id"`
		Title            string   `json:"title"`
		FirstReleaseDate string   `json:"first-release-date"`
		Date             string   `json:"date"`
		ArtistCredit     []credit `json:"artist-credit"`
		ReleaseGroup     *struct {
			ID               string `json:"id"`
			Title            string `json:"title"`
			FirstReleaseDate string `json:"first-release-date"`
		} `json:"release-group"`
	}
	if err := metadata.Fetch(ctx, r.client, endpoint, nil, &data); err != nil {
		return domain.ResolvedItem{}, err
	}
	if data.ID == "" || data.Title == "" {
		return domain.ResolvedItem{}, fmt.Errorf("invalid MusicBrainz response")
	}
	canonicalID, name, date := data.ID, data.Title, data.FirstReleaseDate
	if date == "" {
		date = data.Date
	}
	if entity == "release" {
		if data.ReleaseGroup == nil || !mbidPattern.MatchString(data.ReleaseGroup.ID) || data.ReleaseGroup.Title == "" {
			return domain.ResolvedItem{}, fmt.Errorf("invalid MusicBrainz response")
		}
		canonicalID, entity, name = data.ReleaseGroup.ID, "release-group", data.ReleaseGroup.Title
		if data.ReleaseGroup.FirstReleaseDate != "" {
			date = data.ReleaseGroup.FirstReleaseDate
		}
	}
	title := name
	if len(data.ArtistCredit) > 0 {
		artist := data.ArtistCredit[0].Name
		if artist == "" {
			artist = data.ArtistCredit[0].Artist.Name
		}
		if artist != "" {
			title = artist + " — " + name
		}
	}
	item := domain.CatalogItem{MediaType: media, Provider: "musicbrainz", ProviderID: canonicalID, Title: title, ArtworkKind: domain.ArtworkSquare, ExternalURL: "https://musicbrainz.org/" + entity + "/" + canonicalID}
	if len(date) >= 4 {
		fmt.Sscanf(date[:4], "%d", &item.Year)
	}
	return domain.ResolvedItem{Item: item}, nil
}
