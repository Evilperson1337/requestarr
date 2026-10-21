package jellyfin

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

	"github.com/example/requestarr/internal/domain"
	"github.com/example/requestarr/internal/inventory"
)

const (
	userAgent = "Requestarr/1.0 (+https://github.com/example/requestarr)"
	pageSize  = 500
	maxPages  = 200
	maxBody   = 2 << 20
)

type Client struct {
	baseURL  string
	apiKey   string
	http     *http.Client
	sourceID string
}

// New accepts only an operator-configured server origin. Requests are made to
// the fixed Items endpoint; redirects are disabled to prevent credential leaks.
func New(baseURL, apiKey, sourceID string) (*Client, error) {
	u, err := url.Parse(baseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("invalid Jellyfin server URL")
	}
	if apiKey == "" || sourceID == "" {
		return nil, fmt.Errorf("Jellyfin API key and source ID are required")
	}
	return NewWithHTTPClient(baseURL, apiKey, sourceID, &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }})
}

// NewWithHTTPClient is intended for deterministic transport injection in tests.
func NewWithHTTPClient(baseURL, apiKey, sourceID string, hc *http.Client) (*Client, error) {
	u, err := url.Parse(baseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, fmt.Errorf("invalid Jellyfin server URL")
	}
	if apiKey == "" || sourceID == "" || hc == nil {
		return nil, fmt.Errorf("Jellyfin API key, source ID, and HTTP client are required")
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, sourceID: sourceID, http: hc}, nil
}

func (c *Client) Name() string { return "jellyfin" }

func (c *Client) Sync(ctx context.Context, _ inventory.Cursor) (inventory.ChangeSet, inventory.Cursor, error) {
	items, err := c.Snapshot(ctx)
	if err != nil {
		return inventory.ChangeSet{}, inventory.Cursor{}, err
	}
	return inventory.ChangeSet{Added: items, CompletedAt: time.Now().UTC()}, inventory.Cursor{}, nil
}

func (c *Client) Snapshot(ctx context.Context) ([]inventory.Item, error) {
	var all []inventory.Item
	total := -1
	for page := 0; page < maxPages; page++ {
		offset := page * pageSize
		u, _ := url.Parse(c.baseURL + "/Items")
		q := u.Query()
		q.Set("Recursive", "true")
		q.Set("IncludeItemTypes", "Movie,Series")
		q.Set("Fields", "ProviderIds,ProductionYear")
		q.Set("StartIndex", strconv.Itoa(offset))
		q.Set("Limit", strconv.Itoa(pageSize))
		u.RawQuery = q.Encode()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return nil, fmt.Errorf("create Jellyfin request")
		}
		req.Header.Set("X-Emby-Token", c.apiKey)
		req.Header.Set("User-Agent", userAgent)
		resp, err := c.http.Do(req)
		if err != nil {
			return nil, fmt.Errorf("Jellyfin inventory request failed")
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
		closeErr := resp.Body.Close()
		if readErr != nil || closeErr != nil {
			return nil, fmt.Errorf("read Jellyfin inventory response")
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("Jellyfin inventory returned HTTP %d", resp.StatusCode)
		}
		if len(body) > maxBody {
			return nil, fmt.Errorf("Jellyfin inventory response exceeds size limit")
		}
		mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
		if err != nil || mediaType != "application/json" {
			return nil, fmt.Errorf("Jellyfin inventory response is not application/json")
		}
		var result struct {
			Items []struct {
				ID          string            `json:"Id"`
				Type        string            `json:"Type"`
				Name        string            `json:"Name"`
				Year        int               `json:"ProductionYear"`
				ProviderIDs map[string]string `json:"ProviderIds"`
			} `json:"Items"`
			TotalRecordCount *int `json:"TotalRecordCount"`
			StartIndex       *int `json:"StartIndex"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("decode Jellyfin inventory response")
		}
		if result.Items == nil || result.StartIndex == nil || result.TotalRecordCount == nil {
			return nil, fmt.Errorf("Jellyfin inventory response is missing required fields")
		}
		if *result.StartIndex != offset || *result.TotalRecordCount < 0 || *result.TotalRecordCount > pageSize*maxPages || len(result.Items) > pageSize || offset+len(result.Items) > *result.TotalRecordCount || (total >= 0 && *result.TotalRecordCount != total) {
			return nil, fmt.Errorf("inconsistent Jellyfin inventory pagination")
		}
		if total < 0 {
			total = *result.TotalRecordCount
		}
		for _, raw := range result.Items {
			media := domain.MediaType("")
			switch raw.Type {
			case "Movie":
				media = domain.MediaMovie
			case "Series":
				media = domain.MediaTV
			}
			if media == "" || raw.ID == "" || len(raw.ID) > 256 || len(raw.Name) > 512 {
				return nil, fmt.Errorf("invalid Jellyfin inventory item")
			}
			tmdb := raw.ProviderIDs["Tmdb"]
			ids := map[string]string{}
			if tmdb != "" {
				ids["tmdb"] = tmdb
			}
			all = append(all, inventory.Item{SourceID: c.sourceID, SourceItemID: raw.ID, MediaType: media, Title: raw.Name, Year: raw.Year, ExternalIDs: ids, LastSeenAt: time.Now().UTC()})
		}
		if offset+len(result.Items) == total {
			return all, nil
		}
		if len(result.Items) != pageSize {
			return nil, fmt.Errorf("incomplete Jellyfin inventory pagination")
		}
	}
	return nil, fmt.Errorf("Jellyfin inventory exceeds page limit")
}
