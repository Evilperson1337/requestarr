package oidc

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strings"
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
	// GroupsClaimFound reports whether the ID token had the groups claim at
	// all, so a sign-in can tell "no claim" from "claim with no match".
	GroupsClaimFound bool
	// ClaimNames are the names of the ID token's top-level claims, sorted,
	// for the sign-in diagnostic when the groups claim is missing. Names only:
	// never their values.
	ClaimNames []string
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

// SignInError is a failed sign-in exchange, described by fixed categories an
// operator can act on: the Step that failed and a Reason from a short, fixed
// set. It never holds the code, a token or anything the provider said beyond a
// standard OAuth error code from the list below. Error() stays a fixed phrase
// per step, safe to show or log.
type SignInError struct{ Step, Reason string }

func (e *SignInError) Error() string {
	switch e.Step {
	case StepCodeExchange:
		return "OIDC code exchange failed"
	case StepTokenVerify:
		return "OIDC ID token verification failed"
	case StepClaims:
		return "OIDC claims could not be decoded"
	}
	return "OIDC " + e.Step + " failed"
}

// The steps of the callback exchange, as SignInError.Step.
const (
	StepCodeExchange = "code_exchange"
	StepTokenMissing = "id_token_missing"
	StepTokenVerify  = "id_token_verify"
	StepClaims       = "claims_decode"
	StepSubjectNonce = "subject_or_nonce"
)

// oauthErrorCodes are the standard error codes (RFC 6749 section 5.2) worth
// naming: invalid_client is a wrong client ID or secret, invalid_grant a reused
// or expired code or a redirect address the provider does not match.
var oauthErrorCodes = map[string]bool{
	"invalid_request": true, "invalid_client": true, "invalid_grant": true, "unauthorized_client": true,
	"unsupported_grant_type": true, "invalid_scope": true, "access_denied": true, "server_error": true, "temporarily_unavailable": true,
}

// OAuthErrorCode returns code when it is a standard OAuth error code, and
// "other" for anything else, so nothing a provider chose to say is kept.
func OAuthErrorCode(code string) string {
	if oauthErrorCodes[code] {
		return code
	}
	return "other"
}

// exchangeReason names why the token request failed without keeping its text.
func exchangeReason(err error) string {
	var retrieve *oauth2.RetrieveError
	if errors.As(err, &retrieve) {
		if retrieve.ErrorCode != "" {
			return "oauth_" + OAuthErrorCode(retrieve.ErrorCode)
		}
		if retrieve.Response != nil {
			return fmt.Sprintf("http_%d", retrieve.Response.StatusCode)
		}
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "timeout"
	}
	return "network_or_response"
}

func (c *Client) Exchange(ctx context.Context, code, verifier, nonce string) (*Identity, error) {
	ctx = coreoidc.ClientContext(ctx, c.httpClient)
	tok, err := c.config.Exchange(ctx, code, oauth2.SetAuthURLParam("code_verifier", verifier))
	if err != nil {
		return nil, &SignInError{Step: StepCodeExchange, Reason: exchangeReason(err)}
	}
	raw, ok := tok.Extra("id_token").(string)
	if !ok || raw == "" {
		return nil, &SignInError{Step: StepTokenMissing, Reason: "no_id_token_in_response"}
	}
	id, err := c.verifier.Verify(ctx, raw)
	if err != nil {
		reason := "rejected_check_issuer_client_id_and_signing_keys"
		var expired *coreoidc.TokenExpiredError
		if errors.As(err, &expired) {
			reason = "expired_check_server_clock"
		}
		return nil, &SignInError{Step: StepTokenVerify, Reason: reason}
	}
	var claims struct {
		Subject string `json:"sub"`
		Nonce   string `json:"nonce"`
		Email   string `json:"email"`
		Name    string `json:"name"`
	}
	if err := id.Claims(&claims); err != nil {
		return nil, &SignInError{Step: StepClaims, Reason: "undecodable"}
	}
	var all map[string]json.RawMessage
	if err := id.Claims(&all); err != nil {
		return nil, &SignInError{Step: StepClaims, Reason: "undecodable"}
	}
	if claims.Subject == "" {
		return nil, &SignInError{Step: StepSubjectNonce, Reason: "no_subject"}
	}
	if claims.Nonce != nonce {
		return nil, &SignInError{Step: StepSubjectNonce, Reason: "nonce_mismatch"}
	}
	groupsClaim, found := claimAt(all, c.groupsClaim)
	names := make([]string, 0, len(all))
	for name := range all {
		names = append(names, name)
	}
	sort.Strings(names)
	return &Identity{Subject: claims.Subject, Email: claims.Email, DisplayName: claims.Name, Groups: groupsFrom(groupsClaim), GroupsClaimFound: found, ClaimNames: names}, nil
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

// claimAt finds the groups claim. A claim whose name is exactly the setting
// wins, so a namespaced name such as https://example.com/groups works as is.
// Otherwise a dotted name is a path into nested objects, so Keycloak's role
// claims can be used directly: realm_access.roles, or
// resource_access.<client>.roles for a client's roles.
func claimAt(all map[string]json.RawMessage, name string) (json.RawMessage, bool) {
	if raw, ok := all[name]; ok {
		return raw, true
	}
	parts := strings.Split(name, ".")
	if len(parts) < 2 {
		return nil, false
	}
	current := all
	for i, part := range parts {
		raw, ok := current[part]
		if !ok {
			return nil, false
		}
		if i == len(parts)-1 {
			return raw, true
		}
		current = nil
		if json.Unmarshal(raw, &current) != nil || current == nil {
			return nil, false
		}
	}
	return nil, false
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
