// Package romm is the RomM inventory adapter. RomM is authoritative only for
// games. It implements inventory.Source: it only reads, never touches request
// status, and every failure is an error with no items so a failed sync can
// never look like removals (D015).
//
// The response shapes used here come from RomM's published API docs (platform
// objects with a nullable igdb_id, a paged /api/roms envelope) plus the ROM
// item fields id, name, igdb_id, platform_id and missing_from_fs. Decoding is
// strict: anything that does not fit fails the sync visibly instead of being
// guessed at.
package romm

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

// SourceID is the stable ID of the single configured RomM source (V1 allows one per kind).
const SourceID = sources.RommID

const (
	userAgent       = "Requestarr/1.0 (+https://github.com/evilperson1337/requestarr)"
	defaultPageSize = 200
	maxPages        = 2000
	maxBody         = 16 << 20
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
		return nil, fmt.Errorf("invalid RomM server URL")
	}
	if token == "" || hc == nil {
		return nil, fmt.Errorf("RomM token and HTTP client are required")
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), token: token, http: hc, pageSize: defaultPageSize}, nil
}

func (c *Client) Descriptor() inventory.Descriptor {
	return inventory.Descriptor{ID: SourceID, Kind: "romm", Name: "RomM", MediaTypes: []domain.MediaType{domain.MediaGame}, IdentifierKinds: []string{"igdb"}}
}

// Snapshot lists every platform, then every ROM. Any failure anywhere
// discards everything gathered so far.
func (c *Client) Snapshot(ctx context.Context) (inventory.Snapshot, error) {
	var platforms []struct {
		ID     *int `json:"id"`
		IGDBID *int `json:"igdb_id"`
	}
	if err := c.get(ctx, "/api/platforms", nil, &platforms); err != nil {
		return inventory.Snapshot{}, err
	}
	// RomM platform -> IGDB platform key, the key the catalog's game platforms use.
	// A platform without an IGDB id maps to "", which can satisfy Any but never an explicit selection.
	keys := make(map[int]string, len(platforms))
	for _, p := range platforms {
		if p.ID == nil {
			return inventory.Snapshot{}, fmt.Errorf("invalid RomM platform")
		}
		keys[*p.ID] = ""
		if p.IGDBID != nil && *p.IGDBID > 0 {
			keys[*p.ID] = "igdb:" + strconv.Itoa(*p.IGDBID)
		}
	}
	items := []inventory.Item{}
	total := -1
	for page := 0; page < maxPages; page++ {
		offset := page * c.pageSize
		q := url.Values{"limit": {strconv.Itoa(c.pageSize)}, "offset": {strconv.Itoa(offset)}, "with_char_index": {"false"}, "with_filter_values": {"false"}}
		var res struct {
			Items []struct {
				ID            *int   `json:"id"`
				Name          string `json:"name"`
				IGDBID        *int   `json:"igdb_id"`
				PlatformID    *int   `json:"platform_id"`
				MissingFromFS bool   `json:"missing_from_fs"`
				CreatedAt     string `json:"created_at"`
				CoverLarge    string `json:"path_cover_large"`
				PlatformName  string `json:"platform_display_name"`
			} `json:"items"`
			Total  *int `json:"total"`
			Offset *int `json:"offset"`
		}
		if err := c.get(ctx, "/api/roms", q, &res); err != nil {
			return inventory.Snapshot{}, err
		}
		if res.Items == nil || res.Total == nil || res.Offset == nil || *res.Offset != offset || *res.Total < 0 || (total >= 0 && *res.Total != total) || len(res.Items) > c.pageSize || offset+len(res.Items) > *res.Total {
			return inventory.Snapshot{}, fmt.Errorf("inconsistent RomM ROM pagination")
		}
		total = *res.Total
		for _, r := range res.Items {
			if r.ID == nil || r.PlatformID == nil || r.Name == "" || len(r.Name) > 512 {
				return inventory.Snapshot{}, fmt.Errorf("invalid RomM ROM")
			}
			platformKey, known := keys[*r.PlatformID]
			if !known {
				return inventory.Snapshot{}, fmt.Errorf("RomM ROM on an unknown platform")
			}
			if r.MissingFromFS {
				continue // the file is gone from disk, so it no longer evidences availability
			}
			ids := map[string]string{}
			if r.IGDBID != nil && *r.IGDBID > 0 {
				ids["igdb"] = strconv.Itoa(*r.IGDBID)
			}
			item := inventory.Item{SourceID: SourceID, SourceItemID: strconv.Itoa(*r.ID), MediaType: domain.MediaGame, Title: r.Name, PlatformKey: platformKey, ExternalIDs: ids}
			if added, err := time.Parse(time.RFC3339, r.CreatedAt); err == nil && added.Unix() > 0 {
				item.AddedAt = added.UTC()
			}
			// RomM appends a "?ts=" cache stamp with characters that are not URL-safe; the path alone names the file.
			if ref, _, _ := strings.Cut(r.CoverLarge, "?"); coverPattern.MatchString(ref) {
				item.ArtworkRef = ref
			}
			if name := strings.TrimSpace(r.PlatformName); name != "" && len(name) <= 128 && !strings.ContainsAny(name, "\x00\r\n") {
				item.Detail = name
			}
			items = append(items, item)
		}
		if offset+len(res.Items) == total {
			now := time.Now().UTC()
			for i := range items {
				items[i].LastSeenAt = now
			}
			return inventory.Snapshot{SourceID: SourceID, Items: items, Complete: true, ObservedAt: now}, nil
		}
		if len(res.Items) != c.pageSize {
			return inventory.Snapshot{}, fmt.Errorf("incomplete RomM ROM pagination")
		}
	}
	return inventory.Snapshot{}, fmt.Errorf("RomM ROM listing exceeds page limit")
}

// get never puts the token in the URL and never returns provider text in an
// error: bodies and transport errors can echo credentials.
func (c *Client) get(ctx context.Context, path string, query url.Values, dst any) error {
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("create RomM request")
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("RomM inventory request failed")
	}
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	closeErr := resp.Body.Close()
	if readErr != nil || closeErr != nil {
		return fmt.Errorf("read RomM inventory response")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("RomM inventory returned HTTP %d", resp.StatusCode)
	}
	if len(body) > maxBody {
		return fmt.Errorf("RomM inventory response exceeds size limit")
	}
	if mt, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		return fmt.Errorf("RomM inventory response is not application/json")
	}
	if err := json.Unmarshal(body, dst); err != nil {
		return fmt.Errorf("decode RomM inventory response")
	}
	return nil
}
