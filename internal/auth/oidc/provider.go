package oidc

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	coreoidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

var (
	ErrIssuerURL         = errors.New("OIDC issuer must use https (loopback http is development-only), without credentials, query or fragment")
	ErrDiscoveryDocument = errors.New("could not read a valid discovery document; check reachability, JSON and the exact issuer URL")
	ErrDiscoveryMetadata = errors.New("discovery metadata contains missing or unsafe authorization, token or JWKS endpoints")
)

type Config struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	Scopes       []string
	GroupsClaim  string
	GroupRoleMap map[string]string
	HTTPTimeout  time.Duration
	AllowHTTP    bool
}

type Identity struct {
	Subject     string
	Email       string
	DisplayName string
	Groups      []string
}

// Provider boundary. The concrete implementation should use standards-compliant
// OIDC discovery and Authorization Code + PKCE where applicable. Keycloak is the
// primary V1 compatibility target.
type Provider interface {
	AuthorizationURL(state, nonce, pkceChallenge string) string
	Exchange(context.Context, string, string, string) (*Identity, error)
}

// Client performs standards-compliant OIDC discovery and ID-token verification.
type Client struct {
	config      *oauth2.Config
	verifier    *coreoidc.IDTokenVerifier
	httpClient  *http.Client
	advertised  []string
	groupsClaim string
}

// RequestScopes returns the scopes to request: the configured ones with
// duplicates removed and openid always first, so an empty setting cannot break
// sign-in.
func RequestScopes(configured []string) []string {
	out := []string{coreoidc.ScopeOpenID}
	seen := map[string]bool{coreoidc.ScopeOpenID: true}
	for _, s := range configured {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// AdvertisedScopes returns the scopes the provider lists in its discovery
// document. known is false when it lists none (the field is optional).
func (c *Client) AdvertisedScopes() (scopes []string, known bool) {
	return c.advertised, len(c.advertised) > 0
}

func New(ctx context.Context, cfg Config, redirectURL string) (*Client, error) {
	if cfg.Issuer == "" || cfg.ClientID == "" || redirectURL == "" {
		return nil, fmt.Errorf("OIDC issuer, client ID, and redirect URL are required")
	}
	if cfg.HTTPTimeout <= 0 {
		cfg.HTTPTimeout = 10 * time.Second
	}
	if !safeURL(cfg.Issuer, cfg.AllowHTTP) {
		return nil, ErrIssuerURL
	}
	client := &http.Client{Timeout: cfg.HTTPTimeout, Transport: safeTransport{allowHTTP: cfg.AllowHTTP}, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("OIDC redirects are not allowed") }}
	ctx = coreoidc.ClientContext(ctx, client)
	p, err := coreoidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, ErrDiscoveryDocument
	}
	var metadata struct {
		JWKS   string   `json:"jwks_uri"`
		Scopes []string `json:"scopes_supported"`
	}
	if p.Claims(&metadata) != nil || !safeURL(p.Endpoint().AuthURL, cfg.AllowHTTP) || !safeURL(p.Endpoint().TokenURL, cfg.AllowHTTP) || !safeURL(metadata.JWKS, cfg.AllowHTTP) {
		return nil, ErrDiscoveryMetadata
	}
	return &Client{httpClient: client, config: &oauth2.Config{ClientID: cfg.ClientID, ClientSecret: cfg.ClientSecret, Endpoint: p.Endpoint(), RedirectURL: redirectURL, Scopes: RequestScopes(cfg.Scopes)}, verifier: p.Verifier(&coreoidc.Config{ClientID: cfg.ClientID}), advertised: metadata.Scopes, groupsClaim: claimName(cfg.GroupsClaim)}, nil
}

func (c *Client) Validate(context.Context, Config) error { return nil }

func (c *Client) AuthorizationURL(state, nonce, challenge string) string {
	return c.config.AuthCodeURL(state, oauth2.SetAuthURLParam("nonce", nonce), oauth2.SetAuthURLParam("code_challenge", challenge), oauth2.SetAuthURLParam("code_challenge_method", "S256"))
}

func (c *Client) Exchange(ctx context.Context, code, verifier, nonce string) (*Identity, error) {
	ctx = coreoidc.ClientContext(ctx, c.httpClient)
	tok, err := c.config.Exchange(ctx, code, oauth2.SetAuthURLParam("code_verifier", verifier))
	if err != nil {
		return nil, errors.New("OIDC code exchange failed")
	}
	raw, ok := tok.Extra("id_token").(string)
	if !ok || raw == "" {
		return nil, fmt.Errorf("OIDC response is missing ID token")
	}
	id, err := c.verifier.Verify(ctx, raw)
	if err != nil {
		return nil, errors.New("OIDC ID token verification failed")
	}
	var claims struct {
		Subject string `json:"sub"`
		Nonce   string `json:"nonce"`
		Email   string `json:"email"`
		Name    string `json:"name"`
	}
	if err := id.Claims(&claims); err != nil {
		return nil, errors.New("OIDC claims could not be decoded")
	}
	var all map[string]json.RawMessage
	if err := id.Claims(&all); err != nil {
		return nil, errors.New("OIDC claims could not be decoded")
	}
	if claims.Subject == "" || claims.Nonce != nonce {
		return nil, fmt.Errorf("OIDC subject or nonce is invalid")
	}
	return &Identity{Subject: claims.Subject, Email: claims.Email, DisplayName: claims.Name, Groups: groupsFrom(all[c.groupsClaim])}, nil
}

func RandomValue() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// RandomUUID returns a random version 4 UUID in canonical text form, for
// primary keys that PostgreSQL stores as UUID.
func RandomUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}

func PKCEChallenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

// DefaultGroupsClaim is the ID-token claim groups are read from unless the
// operator names another.
const DefaultGroupsClaim = "groups"

func claimName(configured string) string {
	if configured == "" {
		return DefaultGroupsClaim
	}
	return configured
}

// groupsFrom reads a groups claim that is a list of text values or a single
// text value. Anything else, and non-text list items, give no groups. The
// result is used for the sign-in decision only.
func groupsFrom(raw json.RawMessage) []string {
	var one string
	if json.Unmarshal(raw, &one) == nil {
		if one == "" {
			return nil
		}
		return []string{one}
	}
	var list []any
	if json.Unmarshal(raw, &list) != nil {
		return nil
	}
	var out []string
	for _, v := range list {
		if s, ok := v.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}
