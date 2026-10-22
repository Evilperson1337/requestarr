package musicbrainz

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/providers/metadata"
)

const (
	// MusicBrainz allows one request per second per client.
	DefaultMinInterval = 1100 * time.Millisecond
	maxAliasPages      = 5 // bounds one lookup; a larger set only means fewer aliases, never wrong ones
	albumPageSize      = 100
	songPageSize       = 25 // each release carries its track list, which MusicBrainz caps per page
)

// Aliaser finds the identifiers a library Source may expose for a catalog item
// whose canonical identity is a MusicBrainz release-group (album) or recording
// (song). Plex tags albums with a release id and tracks with a release-track
// id, so matching needs the releases of the group and the tracks that carry
// the recording.
type Aliaser struct {
	client      *http.Client
	minInterval time.Duration
	mu          sync.Mutex
	last        time.Time
}

func NewAliaser(client *http.Client, minInterval time.Duration) *Aliaser {
	if client == nil {
		client = &http.Client{Timeout: 9 * time.Second}
	}
	return &Aliaser{client: client, minInterval: minInterval}
}

// Aliases returns the item's own MBID plus the related identifiers found. A
// failed lookup returns an error and no aliases. A group with more releases than
// maxAliasPages pages hold is capped instead: the aliases read are still correct,
// the rest simply do not match.
func (a *Aliaser) Aliases(ctx context.Context, ref domain.CatalogRef) ([]string, error) {
	if !mbidPattern.MatchString(ref.ProviderID) {
		return nil, fmt.Errorf("invalid MusicBrainz identifier")
	}
	id := strings.ToLower(ref.ProviderID)
	var query url.Values
	var pageSize int
	switch ref.MediaType {
	case domain.MediaAlbum:
		query, pageSize = url.Values{"release-group": {id}}, albumPageSize
	case domain.MediaSong:
		query, pageSize = url.Values{"recording": {id}, "inc": {"recordings"}}, songPageSize
	default:
		return nil, fmt.Errorf("MusicBrainz aliases apply only to albums and songs")
	}
	seen := map[string]bool{id: true}
	out := []string{id}
	add := func(candidate string) {
		candidate = strings.ToLower(candidate)
		if mbidPattern.MatchString(candidate) && !seen[candidate] {
			seen[candidate] = true
			out = append(out, candidate)
		}
	}
	offset := 0
	for page := 0; page < maxAliasPages; page++ {
		q := url.Values{"fmt": {"json"}, "limit": {strconv.Itoa(pageSize)}, "offset": {strconv.Itoa(offset)}}
		for k, v := range query {
			q[k] = v
		}
		var res struct {
			Count    *int `json:"release-count"`
			Releases []struct {
				ID    string `json:"id"`
				Media []struct {
					Tracks []struct {
						ID        string `json:"id"`
						Recording struct {
							ID string `json:"id"`
						} `json:"recording"`
					} `json:"tracks"`
				} `json:"media"`
			} `json:"releases"`
		}
		if err := a.fetch(ctx, "https://musicbrainz.org/ws/2/release?"+q.Encode(), &res); err != nil {
			return nil, err
		}
		if res.Count == nil || *res.Count < 0 || (len(res.Releases) == 0 && offset < *res.Count) {
			return nil, fmt.Errorf("inconsistent MusicBrainz release listing")
		}
		for _, r := range res.Releases {
			if ref.MediaType == domain.MediaAlbum {
				add(r.ID)
				continue
			}
			for _, m := range r.Media {
				for _, t := range m.Tracks {
					if strings.EqualFold(t.Recording.ID, id) {
						add(t.ID)
					}
				}
			}
		}
		// Advance by what was returned: a page can hold fewer releases than the limit.
		offset += len(res.Releases)
		if offset >= *res.Count {
			break
		}
	}
	return out, nil
}

// fetch spaces requests at least minInterval apart, honouring cancellation.
func (a *Aliaser) fetch(ctx context.Context, endpoint string, dst any) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if wait := a.minInterval - time.Since(a.last); wait > 0 {
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	err := metadata.Fetch(ctx, a.client, endpoint, nil, dst)
	a.last = time.Now()
	if err != nil {
		return fmt.Errorf("MusicBrainz release lookup failed")
	}
	return nil
}
