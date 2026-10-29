// Package plex is the Plex Media Server inventory adapter for movie, TV and
// music libraries. It implements inventory.Source: it only reads, never touches
// request status, and every failure is an error with no items so a failed sync
// can never look like removals (D015).
package plex

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/inventory"
	"github.com/evilperson1337/requestarr/internal/sources"
)

// SourceID is the stable ID of the single configured Plex source (V1 allows one per kind).
const SourceID = sources.PlexID

const (
	userAgent       = "Requestarr/1.0 (+https://github.com/example/requestarr)"
	defaultPageSize = 500
	maxPages        = 2000 // per library enumeration; music tracks run to six figures
	maxLibraries    = 100
	maxBody         = 8 << 20
)

type Client struct {
	baseURL  string
	token    string
	http     *http.Client
	pageSize int
}

// New accepts only an operator-configured server origin. Redirects are
// disabled so the token can never be forwarded to another host.
func New(baseURL, token string) (*Client, error) {
	return NewWithHTTPClient(baseURL, token, &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }})
}

// NewWithHTTPClient is intended for deterministic transport injection in tests.
func NewWithHTTPClient(baseURL, token string, hc *http.Client) (*Client, error) {
	u, err := url.Parse(baseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, fmt.Errorf("invalid Plex server URL")
	}
	if token == "" || hc == nil {
		return nil, fmt.Errorf("Plex token and HTTP client are required")
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), token: token, http: hc, pageSize: defaultPageSize}, nil
}

func (c *Client) Descriptor() inventory.Descriptor {
	return inventory.Descriptor{ID: SourceID, Kind: "plex", Name: "Plex", MediaTypes: []domain.MediaType{domain.MediaMovie, domain.MediaTV, domain.MediaAlbum, domain.MediaSong}, IdentifierKinds: []string{"tmdb", "imdb", "tvdb", "mbid"}}
}

// library is a Plex library section Requestarr reads: its key and Plex type (movie or show).
type library struct{ key, kind string }

type container struct {
	MediaContainer struct {
		TotalSize *int `json:"totalSize"`
		Offset    *int `json:"offset"`
		Directory []struct {
			Key  string `json:"key"`
			Type string `json:"type"`
		} `json:"Directory"`
		Metadata []struct {
			RatingKey string `json:"ratingKey"`
			Title     string `json:"title"`
			Year      int    `json:"year"`
			// Plex sends both "guid" (a string) and "Guid" (an array). encoding/json
			// matches names case-insensitively, so without this field the string
			// would be decoded into the array and fail.
			PlexGUID string `json:"guid"`
			Guid     []struct {
				ID string `json:"id"`
			} `json:"Guid"`
		} `json:"Metadata"`
	} `json:"MediaContainer"`
}

// Snapshot enumerates every movie and show library. Any failure anywhere
// discards everything gathered so far.
func (c *Client) Snapshot(ctx context.Context) (inventory.Snapshot, error) {
	var libs []library
	sections, err := c.get(ctx, "/library/sections", nil)
	if err != nil {
		return inventory.Snapshot{}, err
	}
	if len(sections.MediaContainer.Directory) > maxLibraries {
		return inventory.Snapshot{}, fmt.Errorf("Plex reports too many libraries")
	}
	for _, d := range sections.MediaContainer.Directory {
		if (d.Type == "movie" || d.Type == "show" || d.Type == "artist") && d.Key != "" {
			libs = append(libs, library{key: d.Key, kind: d.Type})
		}
	}
	if len(libs) == 0 {
		return inventory.Snapshot{}, fmt.Errorf("Plex has no movie, TV or music libraries visible to this token")
	}
	items := []inventory.Item{}
	for _, lib := range libs {
		// A music library is read twice: its albums and its tracks are separate
		// enumerations (Plex library types 9 and 10).
		passes := []struct {
			media    domain.MediaType
			plexType string
		}{{domain.MediaMovie, ""}}
		switch lib.kind {
		case "show":
			passes[0].media = domain.MediaTV
		case "artist":
			passes = []struct {
				media    domain.MediaType
				plexType string
			}{{domain.MediaAlbum, "9"}, {domain.MediaSong, "10"}}
		}
		for _, pass := range passes {
			got, err := c.library(ctx, lib.key, pass.media, pass.plexType)
			if err != nil {
				return inventory.Snapshot{}, err
			}
			items = append(items, got...)
		}
	}
	now := time.Now().UTC()
	for i := range items {
		items[i].LastSeenAt = now
	}
	return inventory.Snapshot{SourceID: SourceID, Items: items, Complete: true, ObservedAt: now}, nil
}

func (c *Client) library(ctx context.Context, key string, media domain.MediaType, plexType string) ([]inventory.Item, error) {
	var items []inventory.Item
	total := -1
	for page := 0; page < maxPages; page++ {
		offset := page * c.pageSize
		q := url.Values{"includeGuids": {"1"}, "X-Plex-Container-Start": {strconv.Itoa(offset)}, "X-Plex-Container-Size": {strconv.Itoa(c.pageSize)}}
		if plexType != "" {
			q.Set("type", plexType)
		}
		res, err := c.get(ctx, "/library/sections/"+url.PathEscape(key)+"/all", q)
		if err != nil {
			return nil, err
		}
		mc := res.MediaContainer
		if mc.TotalSize == nil || *mc.TotalSize < 0 || (total >= 0 && *mc.TotalSize != total) || len(mc.Metadata) > c.pageSize || offset+len(mc.Metadata) > *mc.TotalSize {
			return nil, fmt.Errorf("inconsistent Plex library pagination")
		}
		total = *mc.TotalSize
		for _, m := range mc.Metadata {
			if m.RatingKey == "" || len(m.RatingKey) > 256 || len(m.Title) > 512 || m.Title == "" {
				return nil, fmt.Errorf("invalid Plex library item")
			}
			ids := map[string]string{}
			guids := make([]string, 0, len(m.Guid)+1)
			for _, g := range m.Guid {
				guids = append(guids, g.ID)
			}
			// Older music agents carry the MusicBrainz id only in the top-level guid.
			guids = append(guids, m.PlexGUID)
			for _, g := range guids {
				kind, value, ok := strings.Cut(g, "://")
				if !ok || value == "" || len(value) > 256 {
					continue
				}
				switch kind {
				case "tmdb", "imdb", "tvdb":
					ids[kind] = value
				case "mbid":
					ids["mbid"] = strings.ToLower(value)
				}
			}
			items = append(items, inventory.Item{SourceID: SourceID, SourceItemID: m.RatingKey, MediaType: media, Title: m.Title, Year: m.Year, ExternalIDs: ids})
		}
		if offset+len(mc.Metadata) == total {
			return items, nil
		}
		if len(mc.Metadata) != c.pageSize {
			return nil, fmt.Errorf("incomplete Plex library pagination")
		}
	}
	return nil, fmt.Errorf("Plex library exceeds page limit")
}

// get never puts the token in the URL and never returns provider text in an
// error: bodies and transport errors can echo credentials.
func (c *Client) get(ctx context.Context, path string, query url.Values) (container, error) {
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return container{}, fmt.Errorf("create Plex request")
	}
	req.Header.Set("X-Plex-Token", c.token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	resp, err := c.http.Do(req)
	if err != nil {
		return container{}, fmt.Errorf("Plex inventory request failed")
	}
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	closeErr := resp.Body.Close()
	if readErr != nil || closeErr != nil {
		return container{}, fmt.Errorf("read Plex inventory response")
	}
	if resp.StatusCode != http.StatusOK {
		return container{}, fmt.Errorf("Plex inventory returned HTTP %d", resp.StatusCode)
	}
	if len(body) > maxBody {
		return container{}, fmt.Errorf("Plex inventory response exceeds size limit")
	}
	if mt, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		return container{}, fmt.Errorf("Plex inventory response is not application/json")
	}
	var out container
	if err := json.Unmarshal(body, &out); err != nil {
		return container{}, fmt.Errorf("decode Plex inventory response")
	}
	return out, nil
}
