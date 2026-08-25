package audiobookshelf

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"git.example.com/evilperson1337/Requestarr/internal/auth/oidc"
	"git.example.com/evilperson1337/Requestarr/internal/inventory"
	"git.example.com/evilperson1337/Requestarr/internal/strictjson"
)

const probeTimeout = 10 * time.Second

// ProbeOptions allows tests and deployments to provide the same pinned DNS and
// dialing controls as the reader. It has no repository, token provider, or
// source configuration dependency.
type ProbeOptions struct {
	Resolver     Resolver
	Dialer       Dialer
	AllowedCIDRs []netip.Prefix
}

// Probe is a stateless Audiobookshelf connection tester.
type Probe struct {
	resolver Resolver
	dialer   Dialer
	allowed  []netip.Prefix
}

func NewProbe(options ProbeOptions) *Probe {
	resolver := options.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	dialer := options.Dialer
	if dialer == nil {
		dialer = &net.Dialer{}
	}
	return &Probe{resolver: resolver, dialer: dialer, allowed: append([]netip.Prefix(nil), options.AllowedCIDRs...)}
}

// Probe makes exactly one GET request to /api/libraries. It never enumerates
// items and has no persistence, audit, job, or configuration-read behavior.
func (p *Probe) Probe(ctx context.Context, request inventory.ConnectionProbeRequest) (inventory.ConnectionProbeResult, error) {
	if p == nil || p.resolver == nil || p.dialer == nil || ctx == nil || ctx.Err() != nil {
		return inventory.ConnectionProbeResult{}, inventory.ErrProbeTesterUnavailable
	}
	baseURL, err := parseBaseURL(request.EndpointURL)
	if err != nil || !safeText(request.Credential, 8192) {
		return inventory.ConnectionProbeResult{}, inventory.ErrProbeInvalidDestination
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	addresses, err := p.resolve(ctx, baseURL)
	if err != nil {
		return inventory.ConnectionProbeResult{}, err
	}
	client := newPinnedHTTPClient(baseURL, addresses, p.dialer)
	defer client.CloseIdleConnections()
	return p.libraries(ctx, client, baseURL, request.Credential)
}

func (p *Probe) resolve(ctx context.Context, baseURL *url.URL) ([]netip.Addr, error) {
	host := baseURL.Hostname()
	if literal, err := netip.ParseAddr(host); err == nil {
		if p.allowedAddress(baseURL, literal) {
			return []netip.Addr{literal.Unmap()}, nil
		}
		return nil, inventory.ErrProbeInvalidDestination
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	addresses, err := p.resolver.LookupNetIP(lookupCtx, "ip", host)
	if err != nil || len(addresses) == 0 {
		return nil, inventory.ErrProbeConnectionFailed
	}
	approved := make([]netip.Addr, 0, len(addresses))
	seen := make(map[netip.Addr]struct{}, len(addresses))
	for _, address := range addresses {
		address = address.Unmap()
		if !p.allowedAddress(baseURL, address) {
			return nil, inventory.ErrProbeInvalidDestination
		}
		if _, ok := seen[address]; !ok {
			seen[address] = struct{}{}
			approved = append(approved, address)
		}
	}
	return approved, nil
}

func (p *Probe) allowedAddress(baseURL *url.URL, address netip.Addr) bool {
	if !address.IsValid() || address.Zone() != "" {
		return false
	}
	for _, prefix := range p.allowed {
		if prefix.Contains(address) {
			return true
		}
	}
	return baseURL.Scheme == "https" && oidc.PublicAddress(address)
}

func (p *Probe) libraries(ctx context.Context, client *http.Client, baseURL *url.URL, credential string) (inventory.ConnectionProbeResult, error) {
	target := *baseURL
	target.Path = strings.TrimRight(baseURL.Path, "/") + "/api/libraries"
	target.RawQuery = ""
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return inventory.ConnectionProbeResult{}, inventory.ErrProbeTesterUnavailable
	}
	httpRequest.Header.Set("Authorization", "Bearer "+credential)
	httpRequest.Header.Set("Accept", "application/json")
	httpRequest.Header.Set("User-Agent", userAgent)
	response, err := client.Do(httpRequest)
	if err != nil {
		return inventory.ConnectionProbeResult{}, inventory.ErrProbeConnectionFailed
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return inventory.ConnectionProbeResult{}, inventory.ErrProbeAuthenticationFailed
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if response.StatusCode != http.StatusOK || err != nil || mediaType != "application/json" {
		return inventory.ConnectionProbeResult{}, inventory.ErrProbeResponseInvalid
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil || len(data) > maxResponseBytes || !utf8.Valid(data) || strictjson.RejectDuplicateKeys(data) != nil {
		return inventory.ConnectionProbeResult{}, inventory.ErrProbeResponseInvalid
	}
	var libraries []libraryDTO
	decoder := json.NewDecoder(bytes.NewReader(data))
	if decoder.Decode(&libraries) != nil || !errors.Is(decoder.Decode(&struct{}{}), io.EOF) || len(libraries) > maxLibraries {
		return inventory.ConnectionProbeResult{}, inventory.ErrProbeResponseInvalid
	}
	result := inventory.ConnectionProbeResult{Libraries: make([]inventory.RemoteLibrary, 0, len(libraries))}
	seen := make(map[string]struct{}, len(libraries))
	for _, library := range libraries {
		if !safeText(library.ID, 256) || !safeText(library.Name, 512) {
			return inventory.ConnectionProbeResult{}, inventory.ErrProbeResponseInvalid
		}
		if _, exists := seen[library.ID]; exists {
			return inventory.ConnectionProbeResult{}, inventory.ErrProbeResponseInvalid
		}
		seen[library.ID] = struct{}{}
		result.Libraries = append(result.Libraries, inventory.RemoteLibrary{ID: library.ID, Name: library.Name})
	}
	return result, nil
}

var _ inventory.ConnectionProbe = (*Probe)(nil)
