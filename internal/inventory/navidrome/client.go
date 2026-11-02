// Package navidrome implements the OpenSubsonic inventory adapter for Navidrome.
// It is authoritative for music only and never treats a failed enumeration as
// an empty library.
package navidrome

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
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

const SourceID = sources.NavidromeID

const (
	apiVersion = "1.16.1"
	clientName = "Requestarr"
	pageSize   = 500
	maxPages   = 200
	maxAlbums  = pageSize * maxPages
	maxItems   = 500_000
	maxBody    = 8 << 20
)

type Client struct {
	baseURL  string
	username string
	password string
	http     *http.Client
}

func New(baseURL, username, password string) (*Client, error) {
	return NewWithHTTPClient(baseURL, username, password, &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	})
}

func NewWithHTTPClient(baseURL, username, password string, hc *http.Client) (*Client, error) {
	u, err := url.Parse(baseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, fmt.Errorf("invalid Navidrome server URL")
	}
	if username == "" || password == "" || hc == nil {
		return nil, fmt.Errorf("Navidrome username, password, and HTTP client are required")
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), username: username, password: password, http: hc}, nil
}

func (c *Client) Descriptor() inventory.Descriptor {
	return inventory.Descriptor{
		ID:              SourceID,
		Kind:            "navidrome",
		Name:            "Navidrome",
		MediaTypes:      []domain.MediaType{domain.MediaAlbum, domain.MediaSong},
		IdentifierKinds: []string{"musicbrainz"},
	}
}

type response struct {
	Subsonic struct {
		Status string `json:"status"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		MusicFolders *struct {
			Folders []struct {
				ID   int    `json:"id"`
				Name string `json:"name"`
			} `json:"musicFolder"`
		} `json:"musicFolders"`
		AlbumList *struct {
			Albums []album `json:"album"`
		} `json:"albumList2"`
		Album *album `json:"album"`
	} `json:"subsonic-response"`
}

type album struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Artist        string `json:"artist"`
	Year          int    `json:"year"`
	SongCount     int    `json:"songCount"`
	MusicBrainzID string `json:"musicBrainzId"`
	Songs         []song `json:"song"`
}

type song struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	Artist        string `json:"artist"`
	Year          int    `json:"year"`
	MusicBrainzID string `json:"musicBrainzId"`
	Track         int    `json:"track"`
	DiscNumber    int    `json:"discNumber"`
}

func (c *Client) Snapshot(ctx context.Context) (inventory.Snapshot, error) {
	folders, err := c.get(ctx, "/rest/getMusicFolders.view", nil)
	if err != nil {
		return inventory.Snapshot{}, err
	}
	if folders.Subsonic.MusicFolders == nil || len(folders.Subsonic.MusicFolders.Folders) == 0 {
		return inventory.Snapshot{}, fmt.Errorf("Navidrome has no music folders visible to this account")
	}

	items := make([]inventory.Item, 0)
	seen := make(map[string]struct{})
	for page := 0; page < maxPages; page++ {
		offset := page * pageSize
		q := url.Values{"type": {"alphabeticalByName"}, "size": {strconv.Itoa(pageSize)}, "offset": {strconv.Itoa(offset)}}
		result, err := c.get(ctx, "/rest/getAlbumList2.view", q)
		if err != nil {
			return inventory.Snapshot{}, err
		}
		if result.Subsonic.AlbumList == nil || result.Subsonic.AlbumList.Albums == nil {
			return inventory.Snapshot{}, fmt.Errorf("Navidrome album list response is missing required fields")
		}
		albums := result.Subsonic.AlbumList.Albums
		if len(albums) > pageSize || offset+len(albums) > maxAlbums {
			return inventory.Snapshot{}, fmt.Errorf("Navidrome reports too many albums")
		}
		for _, listed := range albums {
			if listed.ID == "" || len(listed.ID) > 256 || listed.Name == "" || len(listed.Name) > 512 || listed.SongCount < 0 || listed.SongCount > maxAlbums {
				return inventory.Snapshot{}, fmt.Errorf("invalid Navidrome album summary")
			}
			albumResult, err := c.get(ctx, "/rest/getAlbum.view", url.Values{"id": {listed.ID}})
			if err != nil {
				return inventory.Snapshot{}, err
			}
			full := albumResult.Subsonic.Album
			if full == nil || full.ID != listed.ID || full.Name == "" || len(full.Name) > 512 || full.Songs == nil || len(full.Songs) != listed.SongCount {
				return inventory.Snapshot{}, fmt.Errorf("incomplete Navidrome album response")
			}
			if len(items)+1+len(full.Songs) > maxItems {
				return inventory.Snapshot{}, fmt.Errorf("Navidrome inventory exceeds item limit")
			}
			albumItem := makeItem(SourceID, "album:"+full.ID, domain.MediaAlbum, full.Name, full.Year, full.MusicBrainzID)
			albumItem.ExternalURL = c.baseURL + "/app/#/album/" + url.PathEscape(full.ID) + "/show"
			if _, duplicate := seen[albumItem.SourceItemID]; duplicate {
				return inventory.Snapshot{}, fmt.Errorf("duplicate Navidrome inventory item")
			}
			seen[albumItem.SourceItemID] = struct{}{}
			items = append(items, albumItem)
			for _, track := range full.Songs {
				if track.ID == "" || len(track.ID) > 256 || track.Title == "" || len(track.Title) > 512 {
					return inventory.Snapshot{}, fmt.Errorf("invalid Navidrome song")
				}
				year := track.Year
				if year == 0 {
					year = full.Year
				}
				songItem := makeItem(SourceID, "song:"+track.ID, domain.MediaSong, track.Title, year, track.MusicBrainzID)
				songItem.ParentSourceItemID = albumItem.SourceItemID
				if track.Track > 0 && track.Track < 100000 {
					songItem.Position = track.Track
				}
				if track.DiscNumber > 0 && track.DiscNumber < 1000 {
					songItem.Disc = track.DiscNumber
				}
				if _, duplicate := seen[songItem.SourceItemID]; duplicate {
					return inventory.Snapshot{}, fmt.Errorf("duplicate Navidrome inventory item")
				}
				seen[songItem.SourceItemID] = struct{}{}
				items = append(items, songItem)
			}
		}
		if len(albums) < pageSize {
			now := time.Now().UTC()
			for i := range items {
				items[i].LastSeenAt = now
			}
			return inventory.Snapshot{SourceID: SourceID, Items: items, Complete: true, ObservedAt: now}, nil
		}
	}
	return inventory.Snapshot{}, fmt.Errorf("Navidrome album list exceeds page limit")
}

func makeItem(sourceID, sourceItemID string, media domain.MediaType, title string, year int, mbid string) inventory.Item {
	ids := map[string]string{}
	if mbid = strings.ToLower(strings.TrimSpace(mbid)); mbid != "" && len(mbid) <= 256 {
		ids["musicbrainz"] = mbid
	}
	return inventory.Item{SourceID: sourceID, SourceItemID: sourceItemID, MediaType: media, Title: title, Year: year, ExternalIDs: ids}
}

func (c *Client) get(ctx context.Context, path string, query url.Values) (response, error) {
	saltBytes := make([]byte, 16)
	if _, err := rand.Read(saltBytes); err != nil {
		return response{}, fmt.Errorf("create Navidrome authentication token")
	}
	salt := hex.EncodeToString(saltBytes)
	token := md5.Sum([]byte(c.password + salt))
	u, err := url.Parse(c.baseURL + path)
	if err != nil {
		return response{}, fmt.Errorf("create Navidrome request")
	}
	q := u.Query()
	for key, values := range query {
		for _, value := range values {
			q.Add(key, value)
		}
	}
	q.Set("u", c.username)
	q.Set("t", hex.EncodeToString(token[:]))
	q.Set("s", salt)
	q.Set("v", apiVersion)
	q.Set("c", clientName)
	q.Set("f", "json")
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return response{}, fmt.Errorf("create Navidrome request")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Requestarr/1.0 (+https://github.com/example/requestarr)")
	resp, err := c.http.Do(req)
	if err != nil {
		return response{}, fmt.Errorf("Navidrome inventory request failed")
	}
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	closeErr := resp.Body.Close()
	if readErr != nil || closeErr != nil {
		return response{}, fmt.Errorf("read Navidrome inventory response")
	}
	if resp.StatusCode != http.StatusOK {
		return response{}, fmt.Errorf("Navidrome inventory returned HTTP %d", resp.StatusCode)
	}
	if len(body) > maxBody {
		return response{}, fmt.Errorf("Navidrome inventory response exceeds size limit")
	}
	if contentType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type")); err != nil || contentType != "application/json" {
		return response{}, fmt.Errorf("Navidrome inventory response is not application/json")
	}
	var out response
	if err := json.Unmarshal(body, &out); err != nil {
		return response{}, fmt.Errorf("decode Navidrome inventory response")
	}
	if out.Subsonic.Status != "ok" {
		return response{}, fmt.Errorf("Navidrome API returned a failure response")
	}
	return out, nil
}
