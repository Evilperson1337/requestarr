// Package plex reads a narrow, documented Plex JSON inventory contract. It is
// intentionally read-only: it has no source configuration, persistence, jobs,
// reconciliation, or mutation behavior.
package plex

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
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
	maxResponseBytes = 2 << 20
	maxSections      = 100
	maxItems         = 10_000
	pageSize         = 100
	maxPages         = 100
	maxSourceItemID  = 256
	userAgent        = "Requestarr/plex-inventory-v1"
)

var (
	ErrRead           = errors.New("plex inventory read failed")
	ErrAuthentication = errors.New("plex inventory authentication failed")
	ErrUnavailable    = errors.New("plex inventory unavailable")
	ErrResponse       = errors.New("plex inventory response invalid")
	ErrDestination    = errors.New("plex inventory destination denied")
	ErrInsecureHTTP   = errors.New("plex inventory plaintext HTTP denied")
)

type TokenProvider interface {
	PlexToken(context.Context) (string, error)
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
	// AllowLoopbackHTTP permits plaintext HTTP only for development and tests
	// when every resolved destination is a loopback address.
	AllowLoopbackHTTP bool
	Now               func() time.Time
}

type Client struct {
	baseURL   *url.URL
	sourceID  string
	tokens    TokenProvider
	resolver  Resolver
	dialer    Dialer
	allowed   []netip.Prefix
	allowHTTP bool
	now       func() time.Time
}

func NewClient(options Options) (*Client, error) {
	baseURL, err := parseBaseURL(options.BaseURL)
	if err != nil || !safeText(options.SourceID, 256) || options.TokenProvider == nil {
		return nil, ErrRead
	}
	if baseURL.Scheme == "http" && !options.AllowLoopbackHTTP {
		return nil, ErrInsecureHTTP
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
	return &Client{baseURL: baseURL, sourceID: strings.TrimSpace(options.SourceID), tokens: options.TokenProvider, resolver: resolver, dialer: dialer, allowed: append([]netip.Prefix(nil), options.AllowedCIDRs...), allowHTTP: options.AllowLoopbackHTTP, now: now}, nil
}

// Read proves complete pagination for every configured selected section and
// enabled Plex content type. It returns only in-memory observations.
func (c *Client) Read(ctx context.Context, requirements inventory.CompleteReaderRequirements) (inventory.CompleteReaderResult, error) {
	if c == nil || ctx == nil || ctx.Err() != nil {
		return inventory.CompleteReaderResult{}, ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if strings.TrimSpace(requirements.SourceID) != c.sourceID {
		return inventory.CompleteReaderResult{}, ErrRead
	}
	if err := validateRequirements(requirements); err != nil {
		return inventory.CompleteReaderResult{}, ErrRead
	}
	addresses, err := c.resolve(ctx)
	if err != nil {
		return inventory.CompleteReaderResult{}, err
	}
	token, err := c.tokens.PlexToken(ctx)
	if err != nil || !safeText(token, 8192) {
		return inventory.CompleteReaderResult{}, ErrAuthentication
	}
	client := c.httpClient(addresses)
	defer client.CloseIdleConnections()
	sections, err := c.sections(ctx, client, token)
	if err != nil {
		return inventory.CompleteReaderResult{}, err
	}
	byKey := make(map[string]sectionDTO, len(sections))
	for _, section := range sections {
		byKey[section.Key] = section
	}
	result := inventory.CompleteReaderResult{SourceID: c.sourceID, CompletedAt: c.now().UTC(), Complete: true}
	for _, selected := range requirements.SelectedLibraries {
		section, found := byKey[strings.TrimSpace(selected)]
		if !found {
			return inventory.CompleteReaderResult{}, ErrResponse
		}
		for _, mediaType := range requirements.EnabledMediaTypes {
			if !sectionSupports(section.Type, mediaType) {
				result.Coverage = append(result.Coverage, inventory.ObservationCoverage{RemoteLibraryID: section.Key, MediaType: mediaType, Status: inventory.CoverageInapplicable})
				continue
			}
			items, err := c.sectionItems(ctx, client, token, section.Key, mediaType)
			if err != nil {
				return inventory.CompleteReaderResult{}, err
			}
			result.Items = append(result.Items, items...)
			result.Coverage = append(result.Coverage, inventory.ObservationCoverage{RemoteLibraryID: section.Key, MediaType: mediaType, Status: inventory.CoverageVerifiedComplete})
		}
	}
	if err := inventory.ValidateCompleteReaderResult(result, requirements); err != nil {
		return inventory.CompleteReaderResult{}, ErrResponse
	}
	return result, nil
}

func validateRequirements(requirements inventory.CompleteReaderRequirements) error {
	if !safeText(requirements.SourceID, 256) || len(requirements.SelectedLibraries) == 0 || len(requirements.SelectedLibraries) > maxSections || len(requirements.EnabledMediaTypes) == 0 || len(requirements.EnabledMediaTypes) > 6 {
		return errors.New("invalid requirements")
	}
	libraries := make(map[string]struct{}, len(requirements.SelectedLibraries))
	for _, libraryID := range requirements.SelectedLibraries {
		libraryID = strings.TrimSpace(libraryID)
		if !safeText(libraryID, 256) {
			return errors.New("invalid selected library")
		}
		if _, duplicate := libraries[libraryID]; duplicate {
			return errors.New("duplicate selected library")
		}
		libraries[libraryID] = struct{}{}
	}
	types := make(map[domain.MediaType]struct{}, len(requirements.EnabledMediaTypes))
	for _, mediaType := range requirements.EnabledMediaTypes {
		if !mediaType.Valid() {
			return errors.New("invalid enabled media type")
		}
		if _, duplicate := types[mediaType]; duplicate {
			return errors.New("duplicate enabled media type")
		}
		types[mediaType] = struct{}{}
	}
	return nil
}

func (c *Client) resolve(ctx context.Context) ([]netip.Addr, error) {
	host := c.baseURL.Hostname()
	if address, err := netip.ParseAddr(host); err == nil {
		if c.allowedAddress(address) {
			return []netip.Addr{address.Unmap()}, nil
		}
		return nil, ErrDestination
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	addresses, err := c.resolver.LookupNetIP(lookupCtx, "ip", host)
	if err != nil || len(addresses) == 0 {
		return nil, ErrUnavailable
	}
	approved := make([]netip.Addr, 0, len(addresses))
	seen := map[netip.Addr]struct{}{}
	for _, address := range addresses {
		address = address.Unmap()
		if !c.allowedAddress(address) {
			return nil, ErrDestination
		}
		if _, exists := seen[address]; !exists {
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
	if c.baseURL.Scheme == "http" {
		return c.allowHTTP && address.IsLoopback()
	}
	for _, prefix := range c.allowed {
		if prefix.Contains(address) {
			return true
		}
	}
	return c.baseURL.Scheme == "https" && oidc.PublicAddress(address)
}

func (c *Client) httpClient(addresses []netip.Addr) *http.Client {
	port := c.baseURL.Port()
	if port == "" {
		if c.baseURL.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true, TLSHandshakeTimeout: 3 * time.Second, ResponseHeaderTimeout: 5 * time.Second, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
		for _, address := range addresses {
			dialCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			conn, err := c.dialer.DialContext(dialCtx, network, net.JoinHostPort(address.String(), port))
			cancel()
			if err == nil {
				return conn, nil
			}
		}
		return nil, errors.New("approved destination unavailable")
	}}
	return &http.Client{Transport: transport, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

type sectionDTO struct {
	Key   string `json:"key"`
	Title string `json:"title"`
	Type  string `json:"type"`
}
type metadataDTO struct {
	RatingKey string `json:"ratingKey"`
	Type      string `json:"type"`
	Title     string `json:"title"`
	Year      int    `json:"year"`
	GUIDs     []struct {
		ID string `json:"id"`
	} `json:"Guid"`
}
type containerDTO struct {
	Size        int           `json:"size"`
	TotalSize   int           `json:"totalSize"`
	Offset      int           `json:"offset"`
	Directories []sectionDTO  `json:"Directory"`
	Metadata    []metadataDTO `json:"Metadata"`
}
type responseDTO struct {
	MediaContainer containerDTO `json:"MediaContainer"`
}

func (c *Client) sections(ctx context.Context, client *http.Client, token string) ([]sectionDTO, error) {
	var response responseDTO
	if err := c.getJSON(ctx, client, token, "/library/sections", nil, &response); err != nil {
		return nil, err
	}
	sections := response.MediaContainer.Directories
	if len(sections) > maxSections {
		return nil, ErrResponse
	}
	seen := map[string]struct{}{}
	for _, section := range sections {
		if !safeText(section.Key, 256) || !safeText(section.Title, 512) || !knownSectionType(section.Type) {
			return nil, ErrResponse
		}
		if _, exists := seen[section.Key]; exists {
			return nil, ErrResponse
		}
		seen[section.Key] = struct{}{}
	}
	return sections, nil
}

func (c *Client) sectionItems(ctx context.Context, client *http.Client, token, sectionKey string, mediaType domain.MediaType) ([]inventory.CompleteReaderItem, error) {
	var items []inventory.CompleteReaderItem
	total := -1
	for page := 0; page < maxPages; page++ {
		query := url.Values{"X-Plex-Container-Start": []string{strconv.Itoa(page * pageSize)}, "X-Plex-Container-Size": []string{strconv.Itoa(pageSize)}, "type": []string{plexType(mediaType)}}
		var response responseDTO
		if err := c.getJSON(ctx, client, token, "/library/sections/"+url.PathEscape(sectionKey)+"/all", query, &response); err != nil {
			return nil, err
		}
		container := response.MediaContainer
		if container.Offset != page*pageSize || container.Size != len(container.Metadata) || container.Size > pageSize || container.TotalSize < 0 || container.TotalSize > maxItems {
			return nil, ErrResponse
		}
		if total == -1 {
			total = container.TotalSize
		} else if total != container.TotalSize {
			return nil, ErrResponse
		}
		for _, metadata := range container.Metadata {
			item, err := normalizeItem(sectionKey, mediaType, metadata)
			if err != nil {
				return nil, ErrResponse
			}
			items = append(items, item)
		}
		if len(items) > maxItems || len(items) > total {
			return nil, ErrResponse
		}
		if len(items) == total {
			break
		}
		if container.Size != pageSize {
			return nil, ErrResponse
		}
	}
	if len(items) != total {
		return nil, ErrResponse
	}
	return items, nil
}

func normalizeItem(sectionKey string, expected domain.MediaType, metadata metadataDTO) (inventory.CompleteReaderItem, error) {
	if metadata.Type != plexMetadataType(expected) || !safeText(metadata.RatingKey, 256) || !safeText(metadata.Title, 512) || metadata.Year < 0 || metadata.Year > 9999 || len(metadata.GUIDs) > 20 {
		return inventory.CompleteReaderItem{}, errors.New("invalid item")
	}
	sourceItemID := sectionKey + ":" + metadata.RatingKey
	if !safeText(sourceItemID, maxSourceItemID) {
		return inventory.CompleteReaderItem{}, errors.New("invalid qualified item ID")
	}
	externalIDs := make(map[string]string)
	for _, guid := range metadata.GUIDs {
		provider, id, ok := normalizeGUID(guid.ID)
		if !ok {
			continue
		}
		if _, exists := externalIDs[provider]; exists {
			return inventory.CompleteReaderItem{}, errors.New("duplicate guid")
		}
		externalIDs[provider] = id
	}
	return inventory.CompleteReaderItem{RemoteLibraryID: sectionKey, SourceItemID: sourceItemID, MediaType: expected, Title: metadata.Title, Year: metadata.Year, ExternalIDs: externalIDs}, nil
}

func normalizeGUID(value string) (string, string, bool) {
	if !utf8.ValidString(value) || len(value) > 600 {
		return "", "", false
	}
	parts := strings.SplitN(value, "://", 2)
	if len(parts) != 2 || !safeText(parts[1], 512) {
		return "", "", false
	}
	provider := strings.ToLower(parts[0])
	switch provider {
	case "tmdb", "tvdb", "musicbrainz":
		return provider, strings.TrimSpace(parts[1]), true
	default:
		return "", "", false
	}
}

func (c *Client) getJSON(ctx context.Context, client *http.Client, token, path string, query url.Values, destination any) error {
	target := *c.baseURL
	target.Path = strings.TrimRight(c.baseURL.Path, "/") + path
	target.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return ErrRead
	}
	request.Header.Set("X-Plex-Token", token)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", userAgent)
	response, err := client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return ErrUnavailable
		}
		return ErrUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return ErrAuthentication
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if response.StatusCode != http.StatusOK || err != nil || mediaType != "application/json" {
		return ErrResponse
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		if ctx.Err() != nil {
			return ErrUnavailable
		}
		return ErrResponse
	}
	if len(data) > maxResponseBytes || !utf8.Valid(data) || strictjson.RejectDuplicateKeys(data) != nil {
		return ErrResponse
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	if decoder.Decode(destination) != nil || !errors.Is(decoder.Decode(&struct{}{}), io.EOF) {
		return ErrResponse
	}
	return nil
}

func parseBaseURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil || !parsed.IsAbs() || parsed.Opaque != "" || parsed.User != nil || parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Hostname() == "" {
		return nil, errors.New("invalid URL")
	}
	return parsed, nil
}
func safeText(value string, maximum int) bool {
	if !utf8.ValidString(value) || len(value) > maximum || strings.TrimSpace(value) == "" {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func knownSectionType(value string) bool {
	return value == "movie" || value == "show" || value == "artist"
}
func sectionSupports(section string, mediaType domain.MediaType) bool {
	return (section == "movie" && mediaType == domain.MediaMovie) || (section == "show" && mediaType == domain.MediaTV) || (section == "artist" && (mediaType == domain.MediaAlbum || mediaType == domain.MediaSong))
}
func plexType(mediaType domain.MediaType) string {
	switch mediaType {
	case domain.MediaMovie:
		return "1"
	case domain.MediaTV:
		return "2"
	case domain.MediaAlbum:
		return "9"
	case domain.MediaSong:
		return "10"
	default:
		return ""
	}
}

func plexMetadataType(mediaType domain.MediaType) string {
	if mediaType == domain.MediaSong {
		return "track"
	}
	return string(mediaType)
}
