package musicbrainz

import (
	"context"
	"net/url"
	"sort"
	"time"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/providers/metadata"
)

const maxTracks = 200

type preferredKey struct{}

// withPreferredRelease remembers which release a pasted link named, so the
// tracklist comes from that edition rather than one picked for the group.
func withPreferredRelease(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, preferredKey{}, id)
}

func preferredRelease(ctx context.Context) string {
	id, _ := ctx.Value(preferredKey{}).(string)
	return id
}

type groupRelease = struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
	Date   string `json:"date"`
}

// pickRelease chooses the edition whose tracklist represents the album: the one
// the link named, else the earliest official release, else the first listed.
func pickRelease(releases []groupRelease, preferred string) string {
	var official []groupRelease
	for _, release := range releases {
		if !mbidPattern.MatchString(release.ID) {
			continue
		}
		if release.ID == preferred {
			return release.ID
		}
		if release.Status == "Official" {
			official = append(official, release)
		}
	}
	if len(official) > 0 {
		sort.SliceStable(official, func(i, j int) bool {
			a, b := official[i].Date, official[j].Date
			if a == "" || b == "" {
				return a != "" && b == ""
			}
			return a < b
		})
		return official[0].ID
	}
	for _, release := range releases {
		if mbidPattern.MatchString(release.ID) {
			return release.ID
		}
	}
	return ""
}

// tracklist reads one release's tracks. Any failure leaves the list empty.
func (r *Resolver) tracklist(ctx context.Context, releaseID string) []domain.MusicTrack {
	if releaseID == "" {
		return nil
	}
	budget := 5 * time.Second
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline) - 200*time.Millisecond; remaining < budget {
			budget = remaining
		}
	}
	if budget <= 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	var data struct {
		Media []struct {
			Tracks []struct {
				ID     string `json:"id"`
				Title  string `json:"title"`
				Length int64  `json:"length"`
			} `json:"tracks"`
		} `json:"media"`
	}
	if err := apiSpacer.wait(ctx); err != nil {
		return nil
	}
	if err := metadata.FetchRetrying(ctx, r.client, "https://musicbrainz.org/ws/2/release/"+url.PathEscape(releaseID)+"?fmt=json&inc=recordings", nil, &data); err != nil {
		return nil
	}
	var tracks []domain.MusicTrack
	for _, medium := range data.Media {
		for _, t := range medium.Tracks {
			title := clean(t.Title, 512)
			if title == "" || !mbidPattern.MatchString(t.ID) || len(tracks) >= maxTracks {
				continue
			}
			track := domain.MusicTrack{ID: t.ID, Position: len(tracks) + 1, Title: title}
			if t.Length > 0 && t.Length <= 24*60*60*1000 {
				track.LengthSeconds = int(t.Length / 1000)
			}
			tracks = append(tracks, track)
		}
	}
	return tracks
}
