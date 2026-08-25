package oidc

import (
	"bytes"
	"context"
	"crypto/rsa"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"git.example.com/evilperson1337/Requestarr/internal/auth"
	"git.example.com/evilperson1337/Requestarr/internal/strictjson"
	"github.com/go-jose/go-jose/v4"
)

const (
	maxTokenResponse     = 128 * 1024
	maxJWKSResponse      = 256 * 1024
	liveOperationTimeout = 10 * time.Second
)

// LiveClient reuses the discovery destination policy for every OIDC endpoint.
// It does not persist metadata, ID tokens, access tokens, or client secrets.
type LiveClient struct {
	discovery        *DiscoveryClient
	secrets          auth.RuntimeSecretUser
	operationTimeout time.Duration
}

func NewLiveClient(discovery *DiscoveryClient, secrets ...auth.RuntimeSecretUser) *LiveClient {
	client := &LiveClient{discovery: discovery, operationTimeout: liveOperationTimeout}
	if len(secrets) == 1 {
		client.secrets = secrets[0]
	}
	return client
}

func (c *LiveClient) AuthorizationURL(ctx context.Context, config auth.RuntimeConfig, state auth.State, nonce auth.Nonce, verifier auth.PKCEVerifier, redirectURI string) (string, error) {
	ctx, cancel := c.operationContext(ctx)
	defer cancel()
	metadata, err := c.metadata(ctx, config)
	if err != nil {
		return "", err
	}
	endpoint, err := url.Parse(metadata.AuthorizationEndpoint)
	if err != nil {
		return "", errors.New("OIDC authorization endpoint invalid")
	}
	query := endpoint.Query()
	query.Set("response_type", "code")
	query.Set("client_id", config.ClientID)
	query.Set("redirect_uri", redirectURI)
	query.Set("scope", strings.Join(config.Scopes, " "))
	query.Set("state", string(state))
	query.Set("nonce", string(nonce))
	query.Set("code_challenge", auth.PKCEChallenge(verifier))
	query.Set("code_challenge_method", "S256")
	endpoint.RawQuery = query.Encode()
	return endpoint.String(), nil
}

func (c *LiveClient) ExchangeCode(ctx context.Context, config auth.RuntimeConfig, code string, verifier auth.PKCEVerifier, nonce auth.Nonce, redirectURI string) (auth.Identity, error) {
	ctx, cancel := c.operationContext(ctx)
	defer cancel()
	metadata, err := c.metadata(ctx, config)
	if err != nil {
		return auth.Identity{}, err
	}
	values := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirectURI}, "client_id": {config.ClientID}, "code_verifier": {string(verifier)}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, metadata.TokenEndpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return auth.Identity{}, errors.New("create token request")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", userAgent)
	var response []byte
	if config.ClientType == string(ClientConfidential) {
		if c.secrets == nil {
			return auth.Identity{}, errors.New("OIDC client secret unavailable")
		}
		err = c.secrets.UseRuntimeClientSecret(ctx, func(secret []byte) error {
			req.SetBasicAuth(config.ClientID, string(secret))
			response, err = c.do(ctx, metadata.TokenEndpoint, req, maxTokenResponse)
			return err
		})
	} else {
		response, err = c.do(ctx, metadata.TokenEndpoint, req, maxTokenResponse)
	}
	if err != nil {
		return auth.Identity{}, err
	}
	var token struct {
		IDToken   string `json:"id_token"`
		TokenType string `json:"token_type"`
	}
	if err := strictDecode(response, &token); err != nil || token.IDToken == "" {
		return auth.Identity{}, errors.New("OIDC token response invalid")
	}
	return c.verifyIDToken(ctx, metadata, config, nonce, token.IDToken)
}

// operationContext bounds the entire metadata, token, or JWKS protocol path,
// including a peer that sends headers and then trickles a response body.
func (c *LiveClient) operationContext(ctx context.Context) (context.Context, context.CancelFunc) {
	timeout := liveOperationTimeout
	if c != nil && c.operationTimeout > 0 {
		timeout = c.operationTimeout
	}
	return context.WithTimeout(ctx, timeout)
}

func (c *LiveClient) metadata(ctx context.Context, config auth.RuntimeConfig) (discoveryMetadata, error) {
	if c == nil || c.discovery == nil || config.Validate() != nil {
		return discoveryMetadata{}, errors.New("OIDC unavailable")
	}
	issuer, err := parseIssuer(config.Issuer, c.discovery.development)
	if err != nil {
		return discoveryMetadata{}, errors.New("OIDC issuer invalid")
	}
	addresses, err := c.discovery.resolve(ctx, issuer)
	if err != nil {
		return discoveryMetadata{}, err
	}
	u := *issuer
	u.Path = strings.TrimSuffix(u.Path, "/") + "/.well-known/openid-configuration"
	u.RawPath = ""
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return discoveryMetadata{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	body, err := c.doPinned(ctx, issuer, addresses, req, maxDiscoveryResponse)
	if err != nil {
		return discoveryMetadata{}, err
	}
	metadata, err := parseMetadata(body, DiscoveryInput{Issuer: config.Issuer, ClientType: ClientType(config.ClientType)}, c.discovery.development)
	if err != nil {
		return discoveryMetadata{}, err
	}
	return metadata, nil
}

func (c *LiveClient) do(ctx context.Context, endpoint string, req *http.Request, limit int64) ([]byte, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, errors.New("OIDC endpoint invalid")
	}
	addresses, err := c.discovery.resolve(ctx, u)
	if err != nil {
		return nil, err
	}
	return c.doPinned(ctx, u, addresses, req, limit)
}

func (c *LiveClient) doPinned(ctx context.Context, endpoint *url.URL, addresses []netip.Addr, req *http.Request, limit int64) ([]byte, error) {
	transport := &http.Transport{Proxy: nil, DialContext: c.discovery.pinnedDialer(addresses, endpoint.Port()), TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: c.discovery.rootCAs, ServerName: endpoint.Hostname()}, TLSHandshakeTimeout: c.discovery.tlsTimeout, ResponseHeaderTimeout: c.discovery.headerTimeout, DisableKeepAlives: true, ForceAttemptHTTP2: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(req)
	if err != nil {
		return nil, errors.New("OIDC endpoint unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.StatusCode >= 300 && response.StatusCode < 400 {
		return nil, errors.New("OIDC endpoint rejected request")
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return nil, errors.New("OIDC endpoint response invalid")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil || int64(len(body)) > limit {
		return nil, errors.New("OIDC endpoint response invalid")
	}
	return body, nil
}

func strictDecode(data []byte, target any) error {
	if len(data) == 0 || strictjson.RejectDuplicateKeys(data) != nil {
		return errors.New("duplicate JSON")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON")
	}
	return nil
}

func (c *LiveClient) verifyIDToken(ctx context.Context, metadata discoveryMetadata, config auth.RuntimeConfig, nonce auth.Nonce, compact string) (auth.Identity, error) {
	parsed, err := jose.ParseSigned(compact, []jose.SignatureAlgorithm{jose.RS256})
	if err != nil || len(parsed.Signatures) != 1 || parsed.Signatures[0].Header.KeyID == "" {
		return auth.Identity{}, errors.New("ID token signature invalid")
	}
	keysBody, err := c.fetchJWKS(ctx, metadata.JWKSURI)
	if err != nil {
		return auth.Identity{}, err
	}
	var keys jose.JSONWebKeySet
	if err := strictDecode(keysBody, &keys); err != nil || len(keys.Keys) == 0 || len(keys.Keys) > 32 {
		return auth.Identity{}, errors.New("JWKS invalid")
	}
	var key *rsa.PublicKey
	for _, candidate := range keys.Keys {
		if candidate.KeyID == parsed.Signatures[0].Header.KeyID && candidate.Algorithm == string(jose.RS256) {
			if candidate.Use != "" && candidate.Use != "sig" {
				continue
			}
			if public, ok := candidate.Key.(*rsa.PublicKey); ok {
				key = public
				break
			}
		}
	}
	if key == nil {
		return auth.Identity{}, errors.New("ID token key unavailable")
	}
	payload, err := parsed.Verify(key)
	if err != nil {
		return auth.Identity{}, errors.New("ID token signature invalid")
	}
	var claims struct {
		Issuer            string          `json:"iss"`
		Subject           string          `json:"sub"`
		Audience          json.RawMessage `json:"aud"`
		AuthorizedParty   string          `json:"azp"`
		ExpiresAt         int64           `json:"exp"`
		IssuedAt          int64           `json:"iat"`
		Nonce             string          `json:"nonce"`
		Email             string          `json:"email"`
		Name              string          `json:"name"`
		PreferredUsername string          `json:"preferred_username"`
	}
	var rawClaims map[string]json.RawMessage
	if err := strictDecode(payload, &rawClaims); err != nil || json.Unmarshal(payload, &claims) != nil {
		return auth.Identity{}, errors.New("ID token claims invalid")
	}
	audienceOK, multipleAudiences := audienceContains(claims.Audience, config.ClientID)
	if claims.Issuer != config.Issuer || !audienceOK || (multipleAudiences && claims.AuthorizedParty != config.ClientID) || claims.Nonce != string(nonce) || claims.Subject == "" || claims.ExpiresAt <= time.Now().Add(-30*time.Second).Unix() || claims.IssuedAt > time.Now().Add(30*time.Second).Unix() {
		return auth.Identity{}, errors.New("ID token claims invalid")
	}
	groups, err := boundedGroups(rawClaims[config.GroupsClaim])
	if err != nil {
		return auth.Identity{}, errors.New("ID token group claim invalid")
	}
	name := claims.Name
	if name == "" {
		name = claims.PreferredUsername
	}
	if name == "" {
		name = claims.Subject
	}
	return auth.Identity{Issuer: claims.Issuer, Subject: claims.Subject, Email: claims.Email, DisplayName: name, Groups: groups}, nil
}

func boundedGroups(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil || len(values) > 128 {
		return nil, errors.New("invalid group claim")
	}
	for _, value := range values {
		if value == "" || len(value) > 1024 {
			return nil, errors.New("invalid group claim")
		}
	}
	return values, nil
}

func (c *LiveClient) fetchJWKS(ctx context.Context, endpoint string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	return c.do(ctx, endpoint, req, maxJWKSResponse)
}

func audienceContains(raw json.RawMessage, clientID string) (bool, bool) {
	var one string
	if json.Unmarshal(raw, &one) == nil {
		return one == clientID, false
	}
	var many []string
	if json.Unmarshal(raw, &many) != nil {
		return false, false
	}
	for _, value := range many {
		if value == clientID {
			return true, len(many) > 1
		}
	}
	return false, len(many) > 1
}
