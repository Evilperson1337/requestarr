package audiobookshelf

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"git.example.com/evilperson1337/Requestarr/internal/auth/oidc"
	"git.example.com/evilperson1337/Requestarr/internal/domain"
	"git.example.com/evilperson1337/Requestarr/internal/inventory"
	"git.example.com/evilperson1337/Requestarr/internal/strictjson"
)

const (
	apiContractVersion = "2.36.0"
	maxResponseBytes   = 2 << 20
	maxPageSize        = 100
	maxPages           = 100
	maxLibraries       = 100
	maxItems           = 10_000
	userAgent          = "Requestarr/audiobookshelf-v2.36.0"
)

var (
	// ErrRead identifies any failed Audiobookshelf inventory read.
	ErrRead = errors.New("audiobookshelf inventory read failed")
	// ErrAuthentication identifies a rejected or unavailable bearer token.
	ErrAuthentication = errors.New("audiobookshelf reader authentication failed")
	// ErrInvalidResponse identifies an invalid, incomplete, or unexpected remote response.
	ErrInvalidResponse = errors.New("audiobookshelf reader received an invalid response")
	// ErrUnavailable identifies an unavailable remote service or transport.
	ErrUnavailable = errors.New("audiobookshelf reader unavailable")
	// ErrCanceled identifies caller cancellation or deadline expiry. The underlying
	// context error remains available through errors.Is.
	ErrCanceled = errors.New("audiobookshelf reader canceled")
)

// TokenProvider supplies a short-lived bearer token without exposing storage
// details to this read-only adapter.
type TokenProvider interface {
	BearerToken(context.Context) (string, error)
}

type Resolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}
type Dialer interface {
	DialContext(context.Context, string, string) (net.Conn, error)
}

type Options struct {
	BaseURL       string
	SourceID      string
	TokenProvider TokenProvider
	Resolver      Resolver
	Dialer        Dialer
	AllowedCIDRs  []netip.Prefix
	Now           func() time.Time
}

type Client struct {
	baseURL  *url.URL
	sourceID string
	tokens   TokenProvider
	resolver Resolver
	dialer   Dialer
	allowed  []netip.Prefix
	now      func() time.Time
}

func NewClient(options Options) (*Client, error) {
	base, err := parseBaseURL(options.BaseURL)
	if err != nil {
		return nil, readerError(ErrInvalidResponse)
	}
	if !safeText(options.SourceID, 256) || options.TokenProvider == nil {
		return nil, readerError(ErrUnavailable)
	}
	resolver := options.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	dialer := options.Dialer
	if dialer == nil {
		dialer = &net.Dialer{}
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &Client{baseURL: base, sourceID: strings.TrimSpace(options.SourceID), tokens: options.TokenProvider, resolver: resolver, dialer: dialer, allowed: append([]netip.Prefix(nil), options.AllowedCIDRs...), now: now}, nil
}

// Read returns only a fully proven selected-library snapshot. It never commits
// state: callers may inject the existing Phase 5A snapshot service separately.
func (c *Client) Read(ctx context.Context, requirements inventory.CompleteReaderRequirements) (inventory.CompleteReaderResult, error) {
	if c == nil || ctx == nil {
		return inventory.CompleteReaderResult{}, readerError(ErrUnavailable)
	}
	if err := contextReadError(ctx); err != nil {
		return inventory.CompleteReaderResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if strings.TrimSpace(requirements.SourceID) != c.sourceID {
		if cancellation := contextReadError(ctx); cancellation != nil {
			return inventory.CompleteReaderResult{}, cancellation
		}
		return inventory.CompleteReaderResult{}, readerError(ErrUnavailable)
	}
	if err := validateRequirements(requirements); err != nil {
		return inventory.CompleteReaderResult{}, readerError(ErrInvalidResponse)
	}
	addresses, err := c.resolve(ctx)
	if err != nil {
		return inventory.CompleteReaderResult{}, err
	}
	token, err := c.tokens.BearerToken(ctx)
	if err != nil || !safeText(token, 8192) {
		if cancellation := contextReadError(ctx); cancellation != nil {
			return inventory.CompleteReaderResult{}, cancellation
		}
		return inventory.CompleteReaderResult{}, readerError(ErrAuthentication)
	}
	client := c.httpClient(addresses)
	defer client.CloseIdleConnections()
	libraries, err := c.libraries(ctx, client, token)
	if err != nil {
		return inventory.CompleteReaderResult{}, err
	}
	byID := make(map[string]libraryDTO, len(libraries))
	for _, library := range libraries {
		byID[library.ID] = library
	}
	result := inventory.CompleteReaderResult{SourceID: c.sourceID, CompletedAt: c.now().UTC(), Complete: true}
	for _, id := range requirements.SelectedLibraries {
		id = strings.TrimSpace(id)
		library, found := byID[id]
		if !found {
			return inventory.CompleteReaderResult{}, readerError(ErrInvalidResponse)
		}
		if !bookLibrary(library.MediaType) {
			return inventory.CompleteReaderResult{}, readerError(ErrInvalidResponse)
		}
	}
	for _, id := range requirements.SelectedLibraries {
		id = strings.TrimSpace(id)
		items, err := c.libraryItems(ctx, client, token, id)
		if err != nil {
			return inventory.CompleteReaderResult{}, err
		}
		result.Items = append(result.Items, items...)
		result.Coverage = append(result.Coverage, inventory.ObservationCoverage{RemoteLibraryID: id, MediaType: domain.MediaAudiobook, Status: inventory.CoverageVerifiedComplete})
	}
	if err := inventory.ValidateCompleteReaderResult(result, requirements); err != nil {
		return inventory.CompleteReaderResult{}, readerError(ErrInvalidResponse)
	}
	return result, nil
}

func (c *Client) resolve(ctx context.Context) ([]netip.Addr, error) {
	host := c.baseURL.Hostname()
	if literal, err := netip.ParseAddr(host); err == nil {
		if c.allowedAddress(literal) {
			return []netip.Addr{literal.Unmap()}, nil
		}
		return nil, readerError(ErrUnavailable)
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	addresses, err := c.resolver.LookupNetIP(lookupCtx, "ip", host)
	if err != nil || len(addresses) == 0 {
		return nil, readerError(ErrUnavailable)
	}
	seen := map[netip.Addr]struct{}{}
	approved := make([]netip.Addr, 0, len(addresses))
	for _, address := range addresses {
		address = address.Unmap()
		if !c.allowedAddress(address) {
			return nil, readerError(ErrUnavailable)
		}
		if _, ok := seen[address]; !ok {
			seen[address] = struct{}{}
			approved = append(approved, address)
		}
	}
	return approved, nil
}

func (c *Client) allowedAddress(address netip.Addr) bool {
	if !address.IsValid() || address.Zone() != "" {
		return false
	}
	for _, prefix := range c.allowed {
		if prefix.Contains(address) {
			return true
		}
	}
	return c.baseURL.Scheme == "https" && oidc.PublicAddress(address)
}

func (c *Client) httpClient(addresses []netip.Addr) *http.Client {
	return newPinnedHTTPClient(c.baseURL, addresses, c.dialer)
}

// newPinnedHTTPClient centralizes the transport safeguards shared by readers
// and candidate probes. The dialer sees only DNS-approved, pinned addresses;
// system proxies and redirects are never used.
func newPinnedHTTPClient(baseURL *url.URL, addresses []netip.Addr, dialer Dialer) *http.Client {
	port := baseURL.Port()
	if port == "" {
		if baseURL.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true, TLSHandshakeTimeout: 3 * time.Second, ResponseHeaderTimeout: 5 * time.Second, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
		for _, address := range addresses {
			dialCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			conn, err := dialer.DialContext(dialCtx, network, net.JoinHostPort(address.String(), port))
			cancel()
			if err == nil {
				return conn, nil
			}
		}
		return nil, errors.New("approved destination unavailable")
	}}
	return &http.Client{Transport: transport, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

type libraryDTO struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	MediaType string `json:"mediaType"`
}
type itemResponseDTO struct {
	Results []struct {
		ID    string `json:"id"`
		Media struct {
			Metadata struct {
				Title string `json:"title"`
			} `json:"metadata"`
		} `json:"media"`
	} `json:"results"`
	Total int `json:"total"`
	Limit int `json:"limit"`
	Page  int `json:"page"`
}

func (c *Client) libraries(ctx context.Context, client *http.Client, token string) ([]libraryDTO, error) {
	var response []libraryDTO
	if err := c.getJSON(ctx, client, token, "/api/libraries", &response); err != nil {
		return nil, err
	}
	if len(response) > maxLibraries {
		return nil, readerError(ErrInvalidResponse)
	}
	seen := map[string]struct{}{}
	for _, library := range response {
		if !safeText(library.ID, 256) || !safeText(library.Name, 512) {
			return nil, readerError(ErrInvalidResponse)
		}
		if _, ok := seen[library.ID]; ok {
			return nil, readerError(ErrInvalidResponse)
		}
		seen[library.ID] = struct{}{}
	}
	return response, nil
}

func (c *Client) libraryItems(ctx context.Context, client *http.Client, token, libraryID string) ([]inventory.CompleteReaderItem, error) {
	var all []inventory.CompleteReaderItem
	total := -1
	for page := 0; page < maxPages; page++ {
		var response itemResponseDTO
		endpoint := "/api/libraries/" + url.PathEscape(libraryID) + "/items?limit=" + strconv.Itoa(maxPageSize) + "&page=" + strconv.Itoa(page)
		if err := c.getJSON(ctx, client, token, endpoint, &response); err != nil {
			return nil, err
		}
		if response.Page != page || response.Limit != maxPageSize || response.Total < 0 || response.Total > maxItems || len(response.Results) > maxPageSize {
			return nil, readerError(ErrInvalidResponse)
		}
		if total == -1 {
			total = response.Total
		} else if total != response.Total {
			return nil, readerError(ErrInvalidResponse)
		}
		for _, item := range response.Results {
			if !safeText(item.ID, 256) || !safeText(item.Media.Metadata.Title, 512) {
				return nil, readerError(ErrInvalidResponse)
			}
			all = append(all, inventory.CompleteReaderItem{RemoteLibraryID: libraryID, SourceItemID: item.ID, MediaType: domain.MediaAudiobook, Title: item.Media.Metadata.Title})
		}
		if len(all) > maxItems || len(all) > total {
			return nil, readerError(ErrInvalidResponse)
		}
		if len(all) == total {
			break
		}
		if len(response.Results) != maxPageSize {
			return nil, readerError(ErrInvalidResponse)
		}
	}
	if len(all) != total {
		return nil, readerError(ErrInvalidResponse)
	}
	seen := map[string]struct{}{}
	for _, item := range all {
		if _, ok := seen[item.SourceItemID]; ok {
			return nil, readerError(ErrInvalidResponse)
		}
		seen[item.SourceItemID] = struct{}{}
	}
	return all, nil
}

func (c *Client) getJSON(ctx context.Context, client *http.Client, token, path string, destination any) error {
	target := *c.baseURL
	target.Path = strings.TrimRight(c.baseURL.Path, "/") + path
	target.RawQuery = ""
	if index := strings.IndexByte(path, '?'); index >= 0 {
		target.Path = strings.TrimRight(c.baseURL.Path, "/") + path[:index]
		target.RawQuery = path[index+1:]
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return readerError(ErrUnavailable)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", userAgent)
	response, err := client.Do(request)
	if err != nil {
		if cancellation := contextReadError(ctx); cancellation != nil {
			return cancellation
		}
		return readerError(ErrUnavailable)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return readerError(ErrAuthentication)
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if response.StatusCode >= http.StatusInternalServerError {
		return readerError(ErrUnavailable)
	}
	if response.StatusCode != http.StatusOK || err != nil || mediaType != "application/json" {
		return readerError(ErrInvalidResponse)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		if cancellation := contextReadError(ctx); cancellation != nil {
			return cancellation
		}
		return readerError(ErrUnavailable)
	}
	if len(data) > maxResponseBytes || !utf8.Valid(data) || strictjson.RejectDuplicateKeys(data) != nil {
		return readerError(ErrInvalidResponse)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	if decoder.Decode(destination) != nil || !errors.Is(decoder.Decode(&struct{}{}), io.EOF) {
		return readerError(ErrInvalidResponse)
	}
	return nil
}

func bookLibrary(mediaType string) bool {
	return strings.EqualFold(strings.TrimSpace(mediaType), "book") || strings.EqualFold(strings.TrimSpace(mediaType), "audiobook")
}

func readerError(category error) error {
	return fmt.Errorf("%w: %w", ErrRead, category)
}

func contextReadError(ctx context.Context) error {
	if ctx == nil || ctx.Err() == nil {
		return nil
	}
	return fmt.Errorf("%w: %w: %w", ErrRead, ErrCanceled, ctx.Err())
}

func parseBaseURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil || !parsed.IsAbs() || parsed.Opaque != "" || parsed.User != nil || parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Hostname() == "" {
		return nil, errors.New("invalid URL")
	}
	return parsed, nil
}
func safeText(value string, maximum int) bool {
	if !utf8.ValidString(value) || strings.TrimSpace(value) == "" || len(value) > maximum {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
