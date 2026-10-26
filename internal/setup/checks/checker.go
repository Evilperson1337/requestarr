// Package checks implements bounded, non-persistent setup connection probes.
// Discovery checks connectivity and metadata, not interactive OIDC login.
package checks

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"github.com/evilperson1337/requestarr/internal/secrets"
	"github.com/evilperson1337/requestarr/internal/setup"
)

const maxResponse = 1 << 20

var errCheck = errors.New("setup connection check failed")

// Config holds server-only credentials. Never serialize or log this value.
// Client is optional; injected transports must enforce their own dial policy.
type Config struct {
	Runtime          setup.Runtime
	OIDCClientSecret string
	TMDBReadToken    string
	// Secrets, when set, supplies the TMDB token, IGDB client secret and Apprise key (environment, else stored)
	// that the next restart would use; TMDBReadToken and the runtime flag are
	// then ignored for that check.
	Secrets           setup.SecretSources
	IGDBClientSecret  string
	JellyfinAPIKey    string
	PlexToken         string
	RommToken         string
	NavidromeUsername string
	NavidromePassword string
	Client            *http.Client
	// Resolver optionally replaces DNS for the production transport (e.g. local
	// DNS fixtures). It is ignored when Client supplies its own transport.
	Resolver *net.Resolver
}

type Checker struct {
	config Config
	client *http.Client
}

// New copies the supplied client rather than mutating its timeout or redirects.
func New(config Config) *Checker {
	client := http.Client{}
	if config.Client != nil {
		client = *config.Client
	}
	if client.Transport == nil {
		client.Transport = safeTransport(config.Resolver)
	}
	// Setup probes never need browser cookies, including an injected client's jar.
	client.Jar = nil
	if client.Timeout <= 0 || client.Timeout > 10*time.Second {
		client.Timeout = 10 * time.Second
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Checker{config: config, client: &client}
}

func (c *Checker) Check(ctx context.Context, target setup.Target, settings setup.Settings) (setup.CheckCode, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if target == setup.TargetAuthentication {
		return c.authentication(ctx, settings.Authentication)
	}
	if target == setup.TargetTMDB {
		return c.tmdb(ctx, settings.Providers)
	}
	if target == setup.TargetMusicBrainz {
		return c.musicbrainz(ctx, settings.Providers)
	}
	if target == setup.TargetIGDB {
		return c.igdb(ctx, settings.Providers)
	}
	if target == setup.TargetNotifications {
		return c.apprise(ctx, settings.Notifications)
	}
	if kind, ok := setup.LibrarySource(target); ok {
		return c.library(ctx, kind, settings.Libraries)
	}
	if target != setup.TargetGeneral {
		return setup.CheckUnavailable, errCheck
	}
	s := settings.General
	if strings.TrimSpace(s.InstanceName) == "" || s.Timezone == "" || s.PublicURL == "" {
		return setup.CheckIncomplete, nil
	}
	if _, err := time.LoadLocation(s.Timezone); err != nil || !setup.ValidServiceURL(s.PublicURL, c.config.Runtime.Development) {
		return setup.CheckUnavailable, errCheck
	}
	if s.PublicURL != c.config.Runtime.PublicURL {
		return setup.CheckEnvironment, nil
	}
	var result struct {
		Status string `json:"status"`
	}
	if err := c.request(ctx, http.MethodGet, strings.TrimRight(s.PublicURL, "/")+"/healthz", nil, nil, &result); err != nil || result.Status != "ok" {
		return setup.CheckUnavailable, errCheck
	}
	return setup.CheckOK, nil
}

// apprise probes the status endpoint of the operator-configured Apprise API.
// Only the exact REQUESTARR_APPRISE_URL is probed (with the private-address
// exception), so a draft cannot aim the check at an arbitrary internal host.
func (c *Checker) apprise(ctx context.Context, n setup.NotificationSettings) (setup.CheckCode, error) {
	if !n.AppriseEnabled {
		return setup.CheckDisabled, nil
	}
	if n.AppriseURL == "" {
		return setup.CheckIncomplete, nil
	}
	runtime := c.config.Runtime
	_, keyConfigured, err := c.secret(ctx, secrets.AppriseConfigKey, "", runtime.AppriseKeyConfigured)
	if err != nil {
		return setup.CheckUnavailable, errCheck
	}
	if n.AppriseConfigKeyEnv != setup.AppriseConfigKeyEnv || n.AppriseURL != runtime.AppriseURL || !keyConfigured {
		return setup.CheckEnvironment, nil
	}
	if !setup.ValidServiceURL(n.AppriseURL, runtime.Development) {
		return setup.CheckIncomplete, nil
	}
	ctx = context.WithValue(ctx, trustedDialKey{}, true)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(n.AppriseURL, "/")+"/status", nil)
	if err != nil {
		return setup.CheckUnavailable, errCheck
	}
	req.Header.Set("User-Agent", "Requestarr/setup-check")
	resp, err := c.client.Do(req)
	if err != nil {
		return setup.CheckUnavailable, errCheck
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return setup.CheckUnavailable, errCheck
	}
	return setup.CheckOK, nil
}

func (c *Checker) library(ctx context.Context, kind string, libraries setup.LibrariesSettings) (setup.CheckCode, error) {
	source := map[string]setup.SourceSettings{"jellyfin": libraries.Jellyfin, "plex": libraries.Plex, "romm": libraries.Romm, "navidrome": libraries.Navidrome}[kind]
	if !source.Enabled {
		return setup.CheckDisabled, nil
	}
	if source.URL == "" || len(source.MediaTypes) == 0 {
		return setup.CheckIncomplete, nil
	}
	var endpoint string
	headers := map[string]string{}
	query := ""
	switch kind {
	case "jellyfin":
		token, _, err := c.secret(ctx, secrets.JellyfinAPIKey, c.config.JellyfinAPIKey, c.config.JellyfinAPIKey != "")
		if err != nil {
			return setup.CheckUnavailable, errCheck
		}
		if source.TokenEnv != setup.JellyfinAPIKeyEnv || token == "" {
			return setup.CheckEnvironment, nil
		}
		endpoint = strings.TrimRight(source.URL, "/") + "/System/Info"
		headers["X-Emby-Token"] = token
	case "plex":
		token, _, err := c.secret(ctx, secrets.PlexToken, c.config.PlexToken, c.config.PlexToken != "")
		if err != nil {
			return setup.CheckUnavailable, errCheck
		}
		if source.TokenEnv != setup.PlexTokenEnv || token == "" {
			return setup.CheckEnvironment, nil
		}
		endpoint = strings.TrimRight(source.URL, "/") + "/library/sections"
		headers["X-Plex-Token"] = token
	case "romm":
		token, _, err := c.secret(ctx, secrets.RommToken, c.config.RommToken, c.config.RommToken != "")
		if err != nil {
			return setup.CheckUnavailable, errCheck
		}
		if source.TokenEnv != setup.RommTokenEnv || token == "" {
			return setup.CheckEnvironment, nil
		}
		endpoint = strings.TrimRight(source.URL, "/") + "/api/platforms"
		headers["Authorization"] = "Bearer " + token
	case "navidrome":
		if source.UsernameEnv != setup.NavidromeUsernameEnv || source.PasswordEnv != setup.NavidromePasswordEnv || c.config.NavidromeUsername == "" || c.config.NavidromePassword == "" {
			return setup.CheckEnvironment, nil
		}
		saltBytes := make([]byte, 16)
		if _, err := rand.Read(saltBytes); err != nil {
			return setup.CheckUnavailable, errCheck
		}
		salt := hex.EncodeToString(saltBytes)
		token := md5.Sum([]byte(c.config.NavidromePassword + salt))
		q := url.Values{"u": {c.config.NavidromeUsername}, "t": {hex.EncodeToString(token[:])}, "s": {salt}, "v": {"1.16.1"}, "c": {"Requestarr/setup-check"}, "f": {"json"}}
		endpoint = strings.TrimRight(source.URL, "/") + "/rest/ping.view?" + q.Encode()
	default:
		return setup.CheckUnavailable, errCheck
	}
	if !setup.ValidSourceURL(source.URL, c.config.Runtime.Development) {
		return setup.CheckIncomplete, nil
	}
	ctx = context.WithValue(ctx, trustedDialKey{}, true) // Explicit admin check of this exact operator-supplied Source.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+query, nil)
	if err != nil {
		return setup.CheckUnavailable, errCheck
	}
	req.Header.Set("User-Agent", "Requestarr/setup-check")
	req.Header.Set("Accept", "application/json, application/xml")
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return setup.CheckUnavailable, errCheck
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return setup.CheckUnavailable, errCheck
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (64<<10)+1))
	if err != nil || len(data) == 0 || len(data) > 64<<10 {
		return setup.CheckUnavailable, errCheck
	}
	switch kind {
	case "jellyfin", "romm":
		var result any
		if json.Unmarshal(data, &result) != nil || result == nil {
			return setup.CheckUnavailable, errCheck
		}
	case "plex":
		var result struct {
			XMLName xml.Name `xml:"MediaContainer"`
		}
		if xml.Unmarshal(data, &result) != nil || result.XMLName.Local != "MediaContainer" {
			return setup.CheckUnavailable, errCheck
		}
	case "navidrome":
		var result struct {
			Response struct {
				Status string `json:"status"`
			} `json:"subsonic-response"`
		}
		if json.Unmarshal(data, &result) != nil || result.Response.Status != "ok" {
			return setup.CheckUnavailable, errCheck
		}
	}
	return setup.CheckOK, nil
}

// secret returns the value the next restart would use for name. When a
// resolver is wired it is authoritative (environment, else stored); otherwise
// the boot value and flag apply.
func (c *Checker) secret(ctx context.Context, name secrets.Name, boot string, bootConfigured bool) (string, bool, error) {
	if c.config.Secrets == nil {
		return boot, bootConfigured, nil
	}
	r, err := c.config.Secrets.Resolve(ctx, name)
	if err != nil {
		return "", false, err
	}
	return r.Value, r.Value != "", nil
}

func (c *Checker) igdb(ctx context.Context, s setup.ProviderSettings) (setup.CheckCode, error) {
	if !s.IGDBEnabled {
		return setup.CheckDisabled, nil
	}
	if strings.TrimSpace(s.IGDBClientID) == "" {
		return setup.CheckIncomplete, nil
	}
	secret, configured, err := c.secret(ctx, secrets.IGDBClientSecret, c.config.IGDBClientSecret, c.config.Runtime.IGDBSecretConfigured)
	if err != nil {
		return setup.CheckUnavailable, errCheck
	}
	if s.IGDBClientSecretEnv != setup.IGDBSecretEnv || s.IGDBClientID != c.config.Runtime.IGDBClientID || !configured || secret == "" {
		return setup.CheckEnvironment, nil
	}
	form := url.Values{"client_id": {s.IGDBClientID}, "client_secret": {secret}, "grant_type": {"client_credentials"}}
	var token struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
		TokenType   string `json:"token_type"`
	}
	if err := c.request(ctx, http.MethodPost, "https://id.twitch.tv/oauth2/token", strings.NewReader(form.Encode()), map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, &token); err != nil || token.AccessToken == "" || len(token.AccessToken) > 4096 || strings.ContainsAny(token.AccessToken, "\r\n\x00") || !strings.EqualFold(token.TokenType, "bearer") || token.ExpiresIn <= 0 || token.ExpiresIn > 365*24*60*60 {
		return setup.CheckUnavailable, errCheck
	}
	var platforms []struct {
		ID int64 `json:"id"`
	}
	if err := c.request(ctx, http.MethodPost, "https://api.igdb.com/v4/platforms", strings.NewReader("fields id; limit 1;"), map[string]string{"Content-Type": "text/plain", "Authorization": "Bearer " + token.AccessToken, "Client-ID": s.IGDBClientID}, &platforms); err != nil || len(platforms) == 0 {
		return setup.CheckUnavailable, errCheck
	}
	for _, platform := range platforms {
		if platform.ID <= 0 {
			return setup.CheckUnavailable, errCheck
		}
	}
	return setup.CheckOK, nil
}

func (c *Checker) musicbrainz(ctx context.Context, s setup.ProviderSettings) (setup.CheckCode, error) {
	if !s.MusicBrainzEnabled {
		return setup.CheckDisabled, nil
	}
	address, err := mail.ParseAddress(s.MusicBrainzContact)
	if err != nil || address.Address != s.MusicBrainzContact || len(s.MusicBrainzContact) > 254 || strings.ContainsAny(s.MusicBrainzContact, "\r\n\x00") {
		return setup.CheckIncomplete, nil
	}
	const id = "5b11f4ce-a62d-471e-81fc-a69a8278c7da"
	var result struct {
		ID string `json:"id"`
	}
	if err := c.request(ctx, http.MethodGet, "https://musicbrainz.org/ws/2/artist/"+id+"?fmt=json", nil, map[string]string{"User-Agent": "Requestarr/setup-check (" + s.MusicBrainzContact + ")"}, &result); err != nil || result.ID != id {
		return setup.CheckUnavailable, errCheck
	}
	return setup.CheckOK, nil
}

func (c *Checker) tmdb(ctx context.Context, s setup.ProviderSettings) (setup.CheckCode, error) {
	if !s.TMDBEnabled {
		return setup.CheckDisabled, nil
	}
	token := c.config.TMDBReadToken
	configured := c.config.Runtime.TMDBTokenConfigured
	if c.config.Secrets != nil {
		resolved, err := c.config.Secrets.Resolve(ctx, secrets.TMDBReadToken)
		if err != nil {
			return setup.CheckUnavailable, errCheck
		}
		token, configured = resolved.Value, resolved.Value != ""
	}
	if s.TMDBTokenEnv != setup.TMDBTokenEnv || !configured || token == "" {
		return setup.CheckEnvironment, nil
	}
	var result struct {
		Images struct {
			SecureBaseURL string `json:"secure_base_url"`
		} `json:"images"`
	}
	if err := c.request(ctx, http.MethodGet, "https://api.themoviedb.org/3/configuration", nil, map[string]string{"Authorization": "Bearer " + token}, &result); err != nil || !setup.ValidServiceURL(result.Images.SecureBaseURL, false) {
		return setup.CheckUnavailable, errCheck
	}
	return setup.CheckOK, nil
}

func (c *Checker) authentication(ctx context.Context, s setup.AuthenticationSettings) (setup.CheckCode, error) {
	if s.Issuer == "" || strings.TrimSpace(s.ClientID) == "" {
		return setup.CheckIncomplete, nil
	}
	if !setup.ValidServiceURL(s.Issuer, c.config.Runtime.Development) {
		return setup.CheckUnavailable, errCheck
	}
	if !c.allowedIssuer(s.Issuer) {
		return setup.CheckUnavailable, errCheck
	}
	if s.ClientSecretEnv != setup.OIDCSecretEnv {
		return setup.CheckEnvironment, nil
	}
	var discovery struct {
		Issuer        string `json:"issuer"`
		Authorization string `json:"authorization_endpoint"`
		Token         string `json:"token_endpoint"`
		JWKS          string `json:"jwks_uri"`
	}
	if err := c.request(ctx, http.MethodGet, strings.TrimRight(s.Issuer, "/")+"/.well-known/openid-configuration", nil, nil, &discovery); err != nil {
		return setup.CheckUnavailable, errCheck
	}
	if discovery.Issuer != s.Issuer {
		return setup.CheckUnavailable, errCheck
	}
	for _, endpoint := range []string{discovery.Authorization, discovery.Token, discovery.JWKS} {
		if !setup.ValidServiceURL(endpoint, c.config.Runtime.Development) {
			return setup.CheckUnavailable, errCheck
		}
	}
	r := c.config.Runtime
	if s.Issuer != r.OIDCIssuer || s.ClientID != r.OIDCClientID || !r.OIDCSecretConfigured || c.config.OIDCClientSecret == "" {
		return setup.CheckEnvironment, nil
	}
	return setup.CheckOK, nil
}

func (c *Checker) request(ctx context.Context, method, endpoint string, body io.Reader, headers map[string]string, result any) error {
	// Only the exact configured issuer discovery URL and configured public origin
	// receive the operator-trusted private-address exception at the dial boundary.
	if endpoint == strings.TrimRight(c.config.Runtime.OIDCIssuer, "/")+"/.well-known/openid-configuration" || sameOrigin(endpoint, c.config.Runtime.PublicURL) {
		ctx = context.WithValue(ctx, trustedDialKey{}, true)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return errCheck
	}
	req.Header.Set("User-Agent", "Requestarr/setup-check")
	req.Header.Set("Accept", "application/json")
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return errCheck
	}
	defer resp.Body.Close()
	mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" || resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return errCheck
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil || len(data) > maxResponse || json.Unmarshal(data, result) != nil {
		return errCheck
	}
	return nil
}
