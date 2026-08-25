package oidc

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"git.example.com/evilperson1337/Requestarr/internal/strictjson"
)

const (
	PolicyVersion        = 1
	maxDiscoveryResponse = 256 * 1024
	maxMetadataString    = 8192
	maxMetadataArray     = 128
	userAgent            = "Requestarr/oidc-discovery-v1"
)

type ClientType string

const (
	ClientPublic       ClientType = "public"
	ClientConfidential ClientType = "confidential"
)

// DiscoveryInput deliberately contains no client ID, client secret, setup
// credential, scopes, claims, or mappings.
type DiscoveryInput struct {
	Issuer     string
	ClientType ClientType
}

// Evidence is the bounded, non-secret result persisted for one setup revision.
type Evidence struct {
	PolicyVersion  int
	Issuer         string
	MetadataDigest string
}

type FailureKind uint8

const (
	FailureRequired FailureKind = iota + 1
	FailureDestination
	FailureUnreachable
	FailureMetadata
	FailureUnavailable
)

// Failure exposes only a stable category. It never wraps provider, address, or
// parser text that could reflect administrator-controlled input.
type Failure struct{ Kind FailureKind }

func (e *Failure) Error() string { return "OIDC discovery validation failed" }

func failure(kind FailureKind) error { return &Failure{Kind: kind} }

type Resolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}

type Dialer interface {
	DialContext(context.Context, string, string) (net.Conn, error)
}

type Options struct {
	Resolver       Resolver
	Dialer         Dialer
	AllowedCIDRs   []netip.Prefix
	Development    bool
	OverallTimeout time.Duration
	DNSTimeout     time.Duration
	DialTimeout    time.Duration
	TLSTimeout     time.Duration
	HeaderTimeout  time.Duration
	RootCAs        *x509.CertPool
}

type DiscoveryClient struct {
	resolver       Resolver
	dialer         Dialer
	allowedCIDRs   []netip.Prefix
	development    bool
	overallTimeout time.Duration
	dnsTimeout     time.Duration
	dialTimeout    time.Duration
	tlsTimeout     time.Duration
	headerTimeout  time.Duration
	rootCAs        *x509.CertPool
}

func NewDiscoveryClient(options Options) *DiscoveryClient {
	resolver := options.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	dialer := options.Dialer
	if dialer == nil {
		dialer = &net.Dialer{}
	}
	return &DiscoveryClient{
		resolver: resolver, dialer: dialer, allowedCIDRs: append([]netip.Prefix(nil), options.AllowedCIDRs...), development: options.Development,
		overallTimeout: durationOr(options.OverallTimeout, 10*time.Second), dnsTimeout: durationOr(options.DNSTimeout, 2*time.Second),
		dialTimeout: durationOr(options.DialTimeout, 3*time.Second), tlsTimeout: durationOr(options.TLSTimeout, 3*time.Second),
		headerTimeout: durationOr(options.HeaderTimeout, 5*time.Second),
		rootCAs:       options.RootCAs,
	}
}

func durationOr(value, fallback time.Duration) time.Duration {
	if value <= 0 {
		return fallback
	}
	return value
}

func (c *DiscoveryClient) Discover(ctx context.Context, input DiscoveryInput) (Evidence, error) {
	if c == nil || c.resolver == nil || c.dialer == nil || ctx.Err() != nil {
		return Evidence{}, failure(FailureUnavailable)
	}
	if input.Issuer == "" || (input.ClientType != ClientPublic && input.ClientType != ClientConfidential) {
		return Evidence{}, failure(FailureRequired)
	}
	issuer, err := parseIssuer(input.Issuer, c.development)
	if err != nil {
		return Evidence{}, failure(FailureDestination)
	}
	discoveryURL := *issuer
	discoveryURL.Path = strings.TrimSuffix(discoveryURL.Path, "/") + "/.well-known/openid-configuration"
	discoveryURL.RawPath = ""

	overallCtx, cancel := context.WithTimeout(ctx, c.overallTimeout)
	defer cancel()
	addresses, err := c.resolve(overallCtx, issuer)
	if err != nil {
		return Evidence{}, err
	}

	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           c.pinnedDialer(addresses, issuer.Port()),
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: c.rootCAs},
		TLSHandshakeTimeout:   c.tlsTimeout,
		ResponseHeaderTimeout: c.headerTimeout,
		DisableKeepAlives:     true,
		ForceAttemptHTTP2:     true,
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequestWithContext(overallCtx, http.MethodGet, discoveryURL.String(), nil)
	if err != nil {
		return Evidence{}, failure(FailureDestination)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	response, err := client.Do(req)
	if err != nil {
		if overallCtx.Err() != nil || ctx.Err() != nil {
			return Evidence{}, failure(FailureUnavailable)
		}
		return Evidence{}, failure(FailureUnreachable)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.StatusCode >= 300 && response.StatusCode < 400 {
		return Evidence{}, failure(FailureUnreachable)
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return Evidence{}, failure(FailureMetadata)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxDiscoveryResponse+1))
	if err != nil {
		return Evidence{}, failure(FailureUnreachable)
	}
	if len(body) > maxDiscoveryResponse {
		return Evidence{}, failure(FailureMetadata)
	}
	metadata, err := parseMetadata(body, input, c.development)
	if err != nil {
		return Evidence{}, err
	}
	digest := sha256.Sum256(body)
	return Evidence{PolicyVersion: PolicyVersion, Issuer: metadata.Issuer, MetadataDigest: hex.EncodeToString(digest[:])}, nil
}

func parseIssuer(raw string, development bool) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil || !parsed.IsAbs() || parsed.Opaque != "" || parsed.User != nil || parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("invalid issuer")
	}
	if !validURLAuthority(parsed) || ambiguousURLPath(parsed) {
		return nil, errors.New("invalid issuer authority or path")
	}
	if parsed.Scheme == "https" {
		return parsed, nil
	}
	if parsed.Scheme == "http" && development && loopbackNameOrLiteral(parsed.Hostname()) {
		return parsed, nil
	}
	return nil, errors.New("unsafe issuer scheme")
}

func loopbackNameOrLiteral(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	address, err := netip.ParseAddr(host)
	return err == nil && address.IsLoopback()
}

func (c *DiscoveryClient) resolve(ctx context.Context, issuer *url.URL) ([]netip.Addr, error) {
	host := issuer.Hostname()
	if literal, err := netip.ParseAddr(host); err == nil {
		if !c.allowedAddress(literal, issuer.Scheme) {
			return nil, failure(FailureDestination)
		}
		return []netip.Addr{literal.Unmap()}, nil
	}
	dnsCtx, cancel := context.WithTimeout(ctx, c.dnsTimeout)
	defer cancel()
	addresses, err := c.resolver.LookupNetIP(dnsCtx, "ip", host)
	if err != nil || len(addresses) == 0 {
		if dnsCtx.Err() != nil || ctx.Err() != nil {
			return nil, failure(FailureUnavailable)
		}
		return nil, failure(FailureUnreachable)
	}
	checked := make([]netip.Addr, 0, len(addresses))
	seen := make(map[netip.Addr]struct{}, len(addresses))
	for _, address := range addresses {
		address = address.Unmap()
		if !address.IsValid() || address.Zone() != "" || !c.allowedAddress(address, issuer.Scheme) {
			return nil, failure(FailureDestination)
		}
		if _, exists := seen[address]; !exists {
			seen[address] = struct{}{}
			checked = append(checked, address)
		}
	}
	return checked, nil
}

func (c *DiscoveryClient) allowedAddress(address netip.Addr, scheme string) bool {
	if c.development && scheme == "http" && address.IsLoopback() {
		return true
	}
	if scheme != "https" {
		return false
	}
	for _, prefix := range c.allowedCIDRs {
		if prefix.Contains(address) {
			return true
		}
	}
	return publicAddress(address)
}

var specialUsePrefixes = []netip.Prefix{
	// IANA IPv4 Special-Purpose Address Registry, reviewed 2026-08-16.
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"), netip.MustParsePrefix("169.254.0.0/16"), netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"), netip.MustParsePrefix("192.31.196.0/24"),
	netip.MustParsePrefix("192.52.193.0/24"), netip.MustParsePrefix("192.88.99.0/24"), netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("192.175.48.0/24"), netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("224.0.0.0/4"), netip.MustParsePrefix("240.0.0.0/4"),
	// IANA IPv6 Special-Purpose Address Registry plus multicast. The globally
	// reachable anycast/protocol exceptions inside 2001::/23 are listed below.
	netip.MustParsePrefix("::/128"), netip.MustParsePrefix("::1/128"), netip.MustParsePrefix("::ffff:0:0/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"), netip.MustParsePrefix("100::/64"), netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"), netip.MustParsePrefix("2002::/16"), netip.MustParsePrefix("2620:4f:8000::/48"),
	netip.MustParsePrefix("3fff::/20"), netip.MustParsePrefix("5f00::/16"), netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("fe80::/10"), netip.MustParsePrefix("ff00::/8"),
}

// These registry entries are globally reachable exceptions nested in the
// otherwise denied 2001::/23 protocol-assignment block. No other special-use
// prefix is implicitly trusted; an operator CIDR is required.
var specialUseGlobalExceptions = []netip.Prefix{
	netip.MustParsePrefix("2001:1::1/128"), netip.MustParsePrefix("2001:1::2/128"),
	netip.MustParsePrefix("2001:1::3/128"), netip.MustParsePrefix("2001:3::/32"),
	netip.MustParsePrefix("2001:4:112::/48"), netip.MustParsePrefix("2001:20::/28"),
	netip.MustParsePrefix("2001:30::/28"),
}

func publicAddress(address netip.Addr) bool {
	if !address.IsValid() || address.Zone() != "" {
		return false
	}
	address = address.Unmap()
	if !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() {
		return false
	}
	for _, prefix := range specialUsePrefixes {
		if prefix.Contains(address) {
			for _, exception := range specialUseGlobalExceptions {
				if exception.Contains(address) {
					return true
				}
			}
			return false
		}
	}
	return true
}

// PublicAddress reports whether an address is globally routable under the
// reviewed special-use registry policy. Other outbound clients may combine it
// with their own explicit, purpose-specific CIDR allowlists.
func PublicAddress(address netip.Addr) bool { return publicAddress(address) }

func (c *DiscoveryClient) pinnedDialer(addresses []netip.Addr, issuerPort string) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, requested string) (net.Conn, error) {
		_, port, err := net.SplitHostPort(requested)
		if err != nil {
			return nil, errors.New("invalid dial destination")
		}
		if issuerPort != "" && port != issuerPort || issuerPort == "" && port != "443" && port != "80" {
			return nil, errors.New("unexpected dial port")
		}
		for _, address := range addresses {
			dialCtx, cancel := context.WithTimeout(ctx, c.dialTimeout)
			connection, dialErr := c.dialer.DialContext(dialCtx, network, net.JoinHostPort(address.String(), port))
			cancel()
			if dialErr == nil {
				return connection, nil
			}
			if ctx.Err() != nil {
				break
			}
		}
		return nil, errors.New("pinned destination unavailable")
	}
}

type discoveryMetadata struct {
	Issuer                            string   `json:"issuer"`
	AuthorizationEndpoint             string   `json:"authorization_endpoint"`
	TokenEndpoint                     string   `json:"token_endpoint"`
	JWKSURI                           string   `json:"jwks_uri"`
	ResponseTypesSupported            []string `json:"response_types_supported"`
	SubjectTypesSupported             []string `json:"subject_types_supported"`
	IDTokenSigningAlgorithmsSupported []string `json:"id_token_signing_alg_values_supported"`
	CodeChallengeMethodsSupported     []string `json:"code_challenge_methods_supported"`
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported"`
}

func parseMetadata(data []byte, input DiscoveryInput, development bool) (discoveryMetadata, error) {
	if len(data) == 0 || !utf8.Valid(data) || strictjson.RejectDuplicateKeys(data) != nil {
		return discoveryMetadata{}, failure(FailureMetadata)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	var metadata discoveryMetadata
	if err := decoder.Decode(&metadata); err != nil {
		return discoveryMetadata{}, failure(FailureMetadata)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return discoveryMetadata{}, failure(FailureMetadata)
	}
	stringsToCheck := []string{metadata.Issuer, metadata.AuthorizationEndpoint, metadata.TokenEndpoint, metadata.JWKSURI}
	for _, value := range stringsToCheck {
		if value == "" || len(value) > maxMetadataString || !utf8.ValidString(value) {
			return discoveryMetadata{}, failure(FailureMetadata)
		}
	}
	arrays := [][]string{metadata.ResponseTypesSupported, metadata.SubjectTypesSupported, metadata.IDTokenSigningAlgorithmsSupported,
		metadata.CodeChallengeMethodsSupported, metadata.TokenEndpointAuthMethodsSupported}
	for _, values := range arrays {
		if len(values) == 0 || len(values) > maxMetadataArray {
			return discoveryMetadata{}, failure(FailureMetadata)
		}
		for _, value := range values {
			if value == "" || len(value) > 256 || !utf8.ValidString(value) {
				return discoveryMetadata{}, failure(FailureMetadata)
			}
		}
	}
	if metadata.Issuer != input.Issuer || !safeMetadataURL(metadata.AuthorizationEndpoint, development) ||
		!safeMetadataURL(metadata.TokenEndpoint, development) || !safeMetadataURL(metadata.JWKSURI, development) ||
		!slices.Contains(metadata.ResponseTypesSupported, "code") ||
		!(slices.Contains(metadata.SubjectTypesSupported, "public") || slices.Contains(metadata.SubjectTypesSupported, "pairwise")) ||
		!slices.Contains(metadata.IDTokenSigningAlgorithmsSupported, "RS256") || !slices.Contains(metadata.CodeChallengeMethodsSupported, "S256") {
		return discoveryMetadata{}, failure(FailureMetadata)
	}
	requiredAuth := "none"
	if input.ClientType == ClientConfidential {
		requiredAuth = "client_secret_basic"
	}
	if !slices.Contains(metadata.TokenEndpointAuthMethodsSupported, requiredAuth) {
		return discoveryMetadata{}, failure(FailureMetadata)
	}
	return metadata, nil
}

func safeMetadataURL(raw string, development bool) bool {
	parsed, err := url.Parse(raw)
	if err != nil || !parsed.IsAbs() || parsed.Opaque != "" || parsed.User != nil || parsed.Host == "" || parsed.Fragment != "" {
		return false
	}
	if !validURLAuthority(parsed) || ambiguousURLPath(parsed) {
		return false
	}
	if parsed.Scheme == "https" {
		return true
	}
	return parsed.Scheme == "http" && development && loopbackNameOrLiteral(parsed.Hostname())
}

func validURLAuthority(parsed *url.URL) bool {
	host := parsed.Hostname()
	if host == "" || strings.Contains(host, "%") || strings.HasSuffix(host, ".") {
		return false
	}
	if strings.Contains(parsed.Host, ":") && net.ParseIP(host) == nil && parsed.Port() == "" {
		return false
	}
	if port := parsed.Port(); port != "" {
		for _, r := range port {
			if r < '0' || r > '9' {
				return false
			}
		}
		if port == "0" {
			return false
		}
	}
	return true
}

func ambiguousURLPath(parsed *url.URL) bool {
	for _, segment := range strings.Split(parsed.EscapedPath(), "/") {
		lower := strings.ToLower(segment)
		if lower == "." || lower == ".." || lower == "%2e" || lower == "%2e%2e" ||
			strings.Contains(lower, "%2f") || strings.Contains(lower, "%5c") {
			return true
		}
	}
	return strings.Contains(parsed.Path, "\\")
}

func (e Evidence) String() string {
	return fmt.Sprintf("Evidence{PolicyVersion:%d Issuer:<redacted> MetadataDigest:%q}", e.PolicyVersion, e.MetadataDigest)
}
