package oidc

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"time"

	coreoidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

type Config struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	Scopes       []string
	GroupsClaim  string
	GroupRoleMap map[string]string
	HTTPTimeout  time.Duration
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
	config   *oauth2.Config
	verifier *coreoidc.IDTokenVerifier
}

func New(ctx context.Context, cfg Config, redirectURL string) (*Client, error) {
	if cfg.Issuer == "" || cfg.ClientID == "" || redirectURL == "" {
		return nil, fmt.Errorf("OIDC issuer, client ID, and redirect URL are required")
	}
	if cfg.HTTPTimeout <= 0 {
		cfg.HTTPTimeout = 10 * time.Second
	}
	ctx = coreoidc.ClientContext(ctx, &http.Client{Timeout: cfg.HTTPTimeout})
	p, err := coreoidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("discover OIDC issuer: %w", err)
	}
	return &Client{config: &oauth2.Config{ClientID: cfg.ClientID, ClientSecret: cfg.ClientSecret, Endpoint: p.Endpoint(), RedirectURL: redirectURL, Scopes: []string{coreoidc.ScopeOpenID, "profile", "email"}}, verifier: p.Verifier(&coreoidc.Config{ClientID: cfg.ClientID})}, nil
}

func (c *Client) Validate(context.Context, Config) error { return nil }

func (c *Client) AuthorizationURL(state, nonce, challenge string) string {
	return c.config.AuthCodeURL(state, oauth2.SetAuthURLParam("nonce", nonce), oauth2.SetAuthURLParam("code_challenge", challenge), oauth2.SetAuthURLParam("code_challenge_method", "S256"))
}

func (c *Client) Exchange(ctx context.Context, code, verifier, nonce string) (*Identity, error) {
	tok, err := c.config.Exchange(ctx, code, oauth2.SetAuthURLParam("code_verifier", verifier))
	if err != nil {
		return nil, fmt.Errorf("exchange OIDC code: %w", err)
	}
	raw, ok := tok.Extra("id_token").(string)
	if !ok || raw == "" {
		return nil, fmt.Errorf("OIDC response is missing ID token")
	}
	id, err := c.verifier.Verify(ctx, raw)
	if err != nil {
		return nil, fmt.Errorf("verify OIDC ID token: %w", err)
	}
	var claims struct {
		Subject string `json:"sub"`
		Nonce   string `json:"nonce"`
		Email   string `json:"email"`
		Name    string `json:"name"`
	}
	if err := id.Claims(&claims); err != nil {
		return nil, fmt.Errorf("decode OIDC claims: %w", err)
	}
	if claims.Subject == "" || claims.Nonce != nonce {
		return nil, fmt.Errorf("OIDC subject or nonce is invalid")
	}
	return &Identity{Subject: claims.Subject, Email: claims.Email, DisplayName: claims.Name}, nil
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
