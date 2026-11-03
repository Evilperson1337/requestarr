// Package audiobookshelf is the Audiobookshelf inventory adapter. It is
// authoritative only for audiobooks, identified by ASIN (D031). It implements
// inventory.Source: it only reads (every request is a GET; it never scans,
// matches, downloads or edits anything in Audiobookshelf), and every failure is
// an error with no items so a failed sync can never look like removals (D015).
//
// The response shapes come from Audiobookshelf's server code: the library list
// ({libraries: [...]}, LibraryController.findAll) and the paged item list
// ({results, total}, LibraryController.getLibraryItems) whose minified items are
// LibraryItem.toOldJSONMinified and Book.toOldJSONMinified. The server's OpenAPI
// spec is stale, so nothing here relies on it. Decoding is strict where a wrong
// answer would change availability, and anything that does not fit fails the
// sync visibly instead of being guessed at.
package audiobookshelf

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

// SourceID is the stable ID of the single configured Audiobookshelf source (V1 allows one per kind).
const SourceID = sources.AudiobookshelfID

const (
	userAgent       = "Requestarr/1.0 (+https://github.com/evilperson1337/requestarr)"
	defaultPageSize = 200
	maxPages        = 2000
	maxLibraries    = 100
	maxBody         = 16 << 20
)

type Client struct {
	baseURL  string
	apiKey   string
	http     *http.Client
	pageSize int
}

// New accepts only an operator-configured server origin. Redirects are
// disabled so the API key can never be forwarded to another host.
func New(baseURL, apiKey string) (*Client, error) {
	return NewWithHTTPClient(baseURL, apiKey, &http.Client{Timeout: 60 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }})
}

// NewWithHTTPClient is intended for deterministic transport injection in tests.
func NewWithHTTPClient(baseURL, apiKey string, hc *http.Client) (*Client, error) {
	u, err := url.Parse(baseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, fmt.Errorf("invalid Audiobookshelf server URL")
	}
	if apiKey == "" || hc == nil {
		return nil, fmt.Errorf("Audiobookshelf API key and HTTP client are required")
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, http: hc, pageSize: defaultPageSize}, nil
}

// Descriptor names the catalog provider "audible" as the identifier kind: an
// item's ASIN is the Audible catalog item's canonical ID.
func (c *Client) Descriptor() inventory.Descriptor {
	return inventory.Descriptor{ID: SourceID, Kind: "audiobookshelf", Name: "Audiobookshelf", MediaTypes: []domain.MediaType{domain.MediaAudiobook}, IdentifierKinds: []string{"audible"}}
}

type library struct {
	ID        string `json:"id"`
	MediaType string `json:"mediaType"`
}

type itemPage struct {
	Results []struct {
		ID        *string `json:"id"`
		AddedAt   int64   `json:"addedAt"`
		IsMissing bool    `json:"isMissing"`
		IsInvalid bool    `json:"isInvalid"`
		MediaType string  `json:"mediaType"`
		Media     *struct {
			Metadata *struct {
				Title         string `json:"title"`
				AuthorName    string `json:"authorName"`
				NarratorName  string `json:"narratorName"`
				ASIN          string `json:"asin"`
				PublishedYear string `json:"publishedYear"`
			} `json:"metadata"`
			NumTracks *int `json:"numTracks"`
		} `json:"media"`
	} `json:"results"`
	Total *int `json:"total"`
}

// Snapshot lists the book libraries, then every item in each. Any failure
// anywhere discards everything gathered so far.
func (c *Client) Snapshot(ctx context.Context) (inventory.Snapshot, error) {
	var libs struct {
		Libraries []library `json:"libraries"`
	}
	if err := c.get(ctx, "/api/libraries", nil, &libs); err != nil {
		return inventory.Snapshot{}, err
	}
	if libs.Libraries == nil || len(libs.Libraries) > maxLibraries {
		return inventory.Snapshot{}, fmt.Errorf("invalid Audiobookshelf library list")
	}
	var books []library
	for _, l := range libs.Libraries {
		if l.ID == "" || len(l.ID) > 128 || strings.ContainsAny(l.ID, "/?#% \x00") {
			return inventory.Snapshot{}, fmt.Errorf("invalid Audiobookshelf library")
		}
		if l.MediaType == "book" {
			books = append(books, l)
		}
	}
	// Having no book library is a misconfigured key (it may have lost access),
	// not an empty collection, so it must not read as every audiobook leaving.
	if len(books) == 0 {
		return inventory.Snapshot{}, fmt.Errorf("Audiobookshelf has no book library this key can read")
	}
	items := []inventory.Item{}
	seen := map[string]bool{}
	for _, lib := range books {
		if err := c.libraryItems(ctx, lib, &items, seen); err != nil {
			return inventory.Snapshot{}, err
		}
	}
	now := time.Now().UTC()
	for i := range items {
		items[i].LastSeenAt = now
	}
	return inventory.Snapshot{SourceID: SourceID, Items: items, Complete: true, ObservedAt: now}, nil
}

func (c *Client) libraryItems(ctx context.Context, lib library, items *[]inventory.Item, seen map[string]bool) error {
	total, fetched := -1, 0
	for page := 0; page < maxPages; page++ {
		q := url.Values{"limit": {strconv.Itoa(c.pageSize)}, "page": {strconv.Itoa(page)}, "minified": {"1"}, "sort": {"addedAt"}}
		var res itemPage
		if err := c.get(ctx, "/api/libraries/"+lib.ID+"/items", q, &res); err != nil {
			return err
		}
		if res.Results == nil || res.Total == nil || *res.Total < 0 || (total >= 0 && *res.Total != total) || len(res.Results) > c.pageSize || fetched+len(res.Results) > *res.Total {
			return fmt.Errorf("inconsistent Audiobookshelf item pagination")
		}
		total = *res.Total
		for _, r := range res.Results {
			if r.ID == nil || *r.ID == "" || len(*r.ID) > 128 || r.Media == nil || r.Media.Metadata == nil || r.Media.NumTracks == nil || r.MediaType != "book" {
				return fmt.Errorf("invalid Audiobookshelf item")
			}
			id, md := *r.ID, r.Media.Metadata
			if seen[id] {
				return fmt.Errorf("Audiobookshelf listed an item twice")
			}
			seen[id] = true
			if title := strings.TrimSpace(md.Title); title == "" || len(title) > 512 {
				return fmt.Errorf("invalid Audiobookshelf item")
			}
			// Missing or unreadable files no longer evidence availability, and an
			// item with no audio track (an ebook) is not an audiobook.
			if r.IsMissing || r.IsInvalid || *r.Media.NumTracks <= 0 {
				continue
			}
			item := inventory.Item{SourceID: SourceID, SourceItemID: id, MediaType: domain.MediaAudiobook, Title: strings.TrimSpace(md.Title),
				ExternalIDs: map[string]string{}, ExternalURL: c.baseURL + "/item/" + url.PathEscape(id), Detail: detail(md.AuthorName, md.NarratorName)}
			// An item without a usable ASIN is still listed; it can only match through the title review.
			if asin, err := domain.ParseASIN(md.ASIN); err == nil {
				item.ExternalIDs["audible"] = asin
			}
			if year, err := strconv.Atoi(strings.TrimSpace(md.PublishedYear)); err == nil && year > 1800 && year < 3000 {
				item.Year = year
			}
			if r.AddedAt > 0 {
				item.AddedAt = time.UnixMilli(r.AddedAt).UTC()
			}
			*items = append(*items, item)
		}
		fetched += len(res.Results)
		if fetched == total {
			return nil
		}
		if len(res.Results) != c.pageSize {
			return fmt.Errorf("incomplete Audiobookshelf item pagination")
		}
	}
	return fmt.Errorf("Audiobookshelf item listing exceeds page limit")
}

// detail is the short library note: the author, and the narrator when known.
func detail(author, narrator string) string {
	author, narrator = clean(author), clean(narrator)
	switch {
	case author != "" && narrator != "":
		return truncate(author+" · read by "+narrator, 128)
	case author != "":
		return truncate(author, 128)
	case narrator != "":
		return truncate("read by "+narrator, 128)
	}
	return ""
}

func clean(s string) string {
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s))
}

func truncate(s string, limit int) string {
	if len(s) > limit {
		s = strings.ToValidUTF8(s[:limit], "")
	}
	return s
}

// get never puts the key in the URL and never returns server text in an error:
// bodies and transport errors can echo credentials.
func (c *Client) get(ctx context.Context, path string, query url.Values, dst any) error {
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("create Audiobookshelf request")
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("Audiobookshelf inventory request failed")
	}
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	closeErr := resp.Body.Close()
	if readErr != nil || closeErr != nil {
		return fmt.Errorf("read Audiobookshelf inventory response")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Audiobookshelf inventory returned HTTP %d", resp.StatusCode)
	}
	if len(body) > maxBody {
		return fmt.Errorf("Audiobookshelf inventory response exceeds size limit")
	}
	if mt, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		return fmt.Errorf("Audiobookshelf inventory response is not application/json")
	}
	if err := json.Unmarshal(body, dst); err != nil {
		return fmt.Errorf("decode Audiobookshelf inventory response")
	}
	return nil
}
