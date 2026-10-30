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
	var inc string
	media := domain.MediaAlbum
	switch parts[0] {
	case "release-group":
		inc = "artists+genres+ratings"
	case "recording":
		inc, media = "artists+genres+ratings+releases", domain.MediaSong
	case "release":
		return r.resolveRelease(ctx, parts[1])
	default:
		return domain.ResolvedItem{}, fmt.Errorf("unsupported MusicBrainz URL")
	}
	entity, id := parts[0], parts[1]
	if !mbidPattern.MatchString(id) {
		return domain.ResolvedItem{}, fmt.Errorf("invalid MusicBrainz identifier")
	}
	endpoint := "https://musicbrainz.org/ws/2/" + entity + "/" + url.PathEscape(id) + "?fmt=json&inc=" + inc
	var data struct {
		ID               string   `json:"id"`
		Title            string   `json:"title"`
		PrimaryType      string   `json:"primary-type"`
		SecondaryTypes   []string `json:"secondary-types"`
		FirstReleaseDate string   `json:"first-release-date"`
		Length           int64    `json:"length"`
		ArtistCredit     []credit `json:"artist-credit"`
		Genres           []struct {
			Name string `json:"name"`
		} `json:"genres"`
		Rating *struct {
			Value      *float64 `json:"value"`
			VotesCount int64    `json:"votes-count"`
		} `json:"rating"`
		Releases []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"releases"`
	}
	if err := metadata.Fetch(ctx, r.client, endpoint, nil, &data); err != nil {
		return domain.ResolvedItem{}, err
	}
	if data.ID == "" || data.Title == "" {
		return domain.ResolvedItem{}, fmt.Errorf("invalid MusicBrainz response")
	}
	title, artists := data.Title, artistNames(data.ArtistCredit)
	if len(artists) > 0 {
		title = artists[0] + " — " + data.Title
	}
	item := domain.CatalogItem{MediaType: media, Provider: "musicbrainz", ProviderID: data.ID, Title: title, ArtworkKind: domain.ArtworkSquare, ExternalURL: "https://musicbrainz.org/" + entity + "/" + data.ID}
	if len(data.FirstReleaseDate) >= 4 {
		fmt.Sscanf(data.FirstReleaseDate[:4], "%d", &item.Year)
	}
	details := &domain.MusicDetails{Artists: artists, PrimaryType: clean(data.PrimaryType, 40), ReleaseDate: fullDate(data.FirstReleaseDate)}
	for _, t := range data.SecondaryTypes {
		if name := clean(t, 40); name != "" && len(details.SecondaryTypes) < 5 {
			details.SecondaryTypes = append(details.SecondaryTypes, name)
		}
	}
	for _, g := range data.Genres {
		if name := clean(g.Name, 80); name != "" && len(details.Genres) < 10 {
			details.Genres = append(details.Genres, name)
		}
	}
	if data.Rating != nil && data.Rating.Value != nil && data.Rating.VotesCount > 0 && data.Rating.VotesCount <= 1000000000 && *data.Rating.Value >= 0 && *data.Rating.Value <= 5 {
		score := *data.Rating.Value
		details.Score, details.RatingCount = &score, data.Rating.VotesCount
	}
	if media == domain.MediaAlbum {
		item.ArtworkURL = "https://coverartarchive.org/release-group/" + data.ID + "/front-500"
	} else {
		if data.Length > 0 && data.Length <= 24*60*60*1000 {
			details.LengthSeconds = int(data.Length / 1000)
		}
		for _, release := range data.Releases {
			if name := clean(release.Title, 256); name != "" && len(details.AppearsOn) < 5 {
				details.AppearsOn = append(details.AppearsOn, name)
			}
			if item.ArtworkURL == "" && mbidPattern.MatchString(release.ID) {
				item.ArtworkURL = "https://coverartarchive.org/release/" + release.ID + "/front-500"
			}
		}
	}
	return domain.ResolvedItem{Item: item, Music: details}, nil
}

// resolveRelease maps a release to its release-group, then reads the group's
// details: the release itself carries no genres or rating for the album.
func (r *Resolver) resolveRelease(ctx context.Context, id string) (domain.ResolvedItem, error) {
	if !mbidPattern.MatchString(id) {
		return domain.ResolvedItem{}, fmt.Errorf("invalid MusicBrainz identifier")
	}
	var data struct {
		ReleaseGroup *struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"release-group"`
	}
	if err := metadata.Fetch(ctx, r.client, "https://musicbrainz.org/ws/2/release/"+url.PathEscape(id)+"?fmt=json&inc=release-groups", nil, &data); err != nil {
		return domain.ResolvedItem{}, err
	}
	if data.ReleaseGroup == nil || !mbidPattern.MatchString(data.ReleaseGroup.ID) || data.ReleaseGroup.Title == "" {
		return domain.ResolvedItem{}, fmt.Errorf("invalid MusicBrainz response")
	}
	return r.Resolve(ctx, &url.URL{Scheme: "https", Host: "musicbrainz.org", Path: "/release-group/" + data.ReleaseGroup.ID})
}

type credit struct {
	Name   string `json:"name"`
	Artist struct {
		Name string `json:"name"`
	} `json:"artist"`
}

func artistNames(credits []credit) []string {
	var out []string
	for _, c := range credits {
		name := c.Name
		if name == "" {
			name = c.Artist.Name
		}
		if name = clean(name, 256); name != "" && len(out) < 10 {
			out = append(out, name)
		}
	}
	return out
}

// fullDate keeps a MusicBrainz date only when it is a complete YYYY-MM-DD;
// partial dates (a bare year) are shown by year alone.
func fullDate(raw string) string {
	if _, err := time.Parse("2006-01-02", raw); err == nil {
		return raw
	}
	return ""
}

func clean(s string, limit int) string {
	s = strings.TrimSpace(strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s))
	if len(s) > limit {
		s = s[:limit]
	}
	return s
}
