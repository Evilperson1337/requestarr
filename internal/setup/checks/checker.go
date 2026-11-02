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
	"net"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"github.com/evilperson1337/requestarr/internal/devlogs"
	"github.com/evilperson1337/requestarr/internal/secrets"
	"github.com/evilperson1337/requestarr/internal/setup"
	"github.com/evilperson1337/requestarr/internal/sources"
)

const maxResponse = 1 << 20

var errCheck = errors.New("setup connection check failed")
var errAuthentication = errors.New("setup authentication rejected")

// Config holds server-only credentials. Never serialize or log this value.
// Client is optional; injected transports must enforce their own dial policy.
type Config struct {
	// Diagnostics is optional and records only allowlisted connection-check facts.
	Diagnostics *devlogs.Recorder
	Runtime     setup.Runtime
	// OIDCClientSecret is environment-only; it is not a stored secret.
	OIDCClientSecret string
	// Secrets supplies every stored-capable secret (environment, else stored):
	// the value the next restart would use. Without it no secret is configured.
	Secrets setup.SecretSources
	Client  *http.Client
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

func (c *Checker) Check(ctx context.Context, target setup.Target, settings setup.Settings) (code setup.CheckCode, err error) {
	started := time.Now()
	defer func() {
		c.config.Diagnostics.Record(target, devlogs.StageCheck, devlogs.Code(code), 0, time.Since(started))
	}()
	ctx = context.WithValue(ctx, diagnosticTargetKey{}, target)
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
	key, err := c.secret(ctx, secrets.AppriseConfigKey)
	if err != nil {
		return setup.CheckUnavailable, errCheck
	}
	if n.AppriseConfigKeyEnv != setup.AppriseConfigKeyEnv || n.AppriseURL != runtime.AppriseURL || key == "" {
		return setup.CheckEnvironment, nil
	}
	if !setup.ValidServiceURL(n.AppriseURL, runtime.Development) {
		return setup.CheckIncomplete, nil
	}
	if err := c.fetch(ctx, fetchSpec{stage: devlogs.StageRequest, method: http.MethodGet, endpoint: strings.TrimRight(n.AppriseURL, "/") + "/status", trusted: true, maxBytes: 64 << 10, discard: true}); err != nil {
		return setup.CheckUnavailable, errCheck
	}
	return setup.CheckOK, nil
}

// libraryProbe is what a connection check does for one Source kind: where to
// ask, how to authenticate, and what a healthy answer looks like. The kind's
// slug, credential fields and URL come from the sources registry.
type libraryProbe struct {
	path        string
	acceptedXML bool // the response is XML, not JSON
	headers     func(token string) map[string]string
	valid       func(data []byte) bool
}

var libraryProbes = map[string]libraryProbe{
	"jellyfin": {path: "/System/Info", headers: func(t string) map[string]string { return map[string]string{"X-Emby-Token": t} }, valid: validJSON},
	"plex": {path: "/library/sections", acceptedXML: true, headers: func(t string) map[string]string { return map[string]string{"X-Plex-Token": t} }, valid: func(data []byte) bool {
		var result struct {
			XMLName xml.Name `xml:"MediaContainer"`
		}
		return xml.Unmarshal(data, &result) == nil && result.XMLName.Local == "MediaContainer"
	}},
	"romm": {path: "/api/platforms", headers: func(t string) map[string]string { return map[string]string{"Authorization": "Bearer " + t} }, valid: validJSON},
	"navidrome": {valid: func(data []byte) bool {
		var result struct {
			Response struct {
				Status string `json:"status"`
			} `json:"subsonic-response"`
		}
		return json.Unmarshal(data, &result) == nil && result.Response.Status == "ok"
	}},
}

func validJSON(data []byte) bool {
	var result any
	return json.Unmarshal(data, &result) == nil && result != nil
}

func (c *Checker) library(ctx context.Context, slug string, libraries setup.LibrariesSettings) (setup.CheckCode, error) {
	kind, known := sources.Lookup(slug)
	probeSpec, hasProbe := libraryProbes[slug]
	if !known || !hasProbe {
		return setup.CheckUnavailable, errCheck
	}
	source := *libraries.Source(slug)
	if !source.Enabled {
		return setup.CheckDisabled, nil
	}
	if source.URL == "" || len(source.MediaTypes) == 0 {
		return setup.CheckIncomplete, nil
	}
	var endpoint string
	headers := map[string]string{}
	if token, hasToken := kind.Credential(sources.RoleToken); hasToken {
		value, err := c.secret(ctx, secrets.Name(token.Secret))
		if err != nil {
			return setup.CheckUnavailable, errCheck
		}
		if source.TokenEnv != token.Env || value == "" {
			return setup.CheckEnvironment, nil
		}
		endpoint = strings.TrimRight(source.URL, "/") + probeSpec.path
		headers = probeSpec.headers(value)
	} else {
		// Username and password: the Subsonic ping with a salted token.
		username := source.Username
		if username == "" {
			username = c.config.Runtime.LibraryUsername(slug)
		}
		passwordCredential, _ := kind.Credential(sources.RolePassword)
		password, err := c.secret(ctx, secrets.Name(passwordCredential.Secret))
		if err != nil {
			return setup.CheckUnavailable, errCheck
		}
		if source.PasswordEnv != passwordCredential.Env || username == "" || password == "" {
			return setup.CheckEnvironment, nil
		}
		saltBytes := make([]byte, 16)
		if _, err := rand.Read(saltBytes); err != nil {
			return setup.CheckUnavailable, errCheck
		}
		salt := hex.EncodeToString(saltBytes)
		token := md5.Sum([]byte(password + salt))
		q := url.Values{"u": {username}, "t": {hex.EncodeToString(token[:])}, "s": {salt}, "v": {"1.16.1"}, "c": {"Requestarr/setup-check"}, "f": {"json"}}
		endpoint = strings.TrimRight(source.URL, "/") + "/rest/ping.view?" + q.Encode()
	}
	if !setup.ValidSourceURL(source.URL, c.config.Runtime.Development) {
		return setup.CheckIncomplete, nil
	}
	// An explicit admin check of this exact operator-supplied Source.
	types := []string{"application/json"}
	accept := "application/json, application/xml"
	if probeSpec.acceptedXML {
		// This probe parses Plex's XML sections response, unlike the JSON
		// inventory client. Plex answers in JSON whenever JSON is acceptable, so
		// the request must ask for XML alone. Both standard XML media types occur.
		accept = "application/xml"
		types = []string{"application/xml", "text/xml"}
	}
	if err := c.fetch(ctx, fetchSpec{stage: devlogs.StageRequest, method: http.MethodGet, endpoint: endpoint, headers: headers, accept: accept,
		trusted: true, types: types, maxBytes: 64 << 10, validate: probeSpec.valid}); err != nil {
		return setup.CheckUnavailable, errCheck
	}
	return setup.CheckOK, nil
}

// secret returns the value the next restart would use for name, from the
// resolver (environment, else stored). It is empty when nothing is configured
// or no resolver is wired.
func (c *Checker) secret(ctx context.Context, name secrets.Name) (string, error) {
	if c.config.Secrets == nil {
		return "", nil
	}
	r, err := c.config.Secrets.Resolve(ctx, name)
	if err != nil {
		return "", err
	}
	return r.Value, nil
}

func (c *Checker) igdb(ctx context.Context, s setup.ProviderSettings) (setup.CheckCode, error) {
	if !s.IGDBEnabled {
		return setup.CheckDisabled, nil
	}
	if strings.TrimSpace(s.IGDBClientID) == "" {
		return setup.CheckIncomplete, nil
	}
	secret, err := c.secret(ctx, secrets.IGDBClientSecret)
	if err != nil {
		return setup.CheckUnavailable, errCheck
	}
	if s.IGDBClientSecretEnv != setup.IGDBSecretEnv || secret == "" ||
		c.config.Runtime.IGDBClientIDFromEnvironment && s.IGDBClientID != c.config.Runtime.IGDBClientID {
		return setup.CheckEnvironment, nil
	}
	form := url.Values{"client_id": {s.IGDBClientID}, "client_secret": {secret}, "grant_type": {"client_credentials"}}
	var token struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
		TokenType   string `json:"token_type"`
	}
	if err := c.requestStage(ctx, devlogs.StageOAuth, http.MethodPost, "https://id.twitch.tv/oauth2/token", strings.NewReader(form.Encode()), map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, &token); err != nil || token.AccessToken == "" || len(token.AccessToken) > 4096 || strings.ContainsAny(token.AccessToken, "\r\n\x00") || !strings.EqualFold(token.TokenType, "bearer") || token.ExpiresIn <= 0 || token.ExpiresIn > 365*24*60*60 {
		return setup.CheckUnavailable, errCheck
	}
	var platforms []struct {
		ID int64 `json:"id"`
	}
	if err := c.requestStage(ctx, devlogs.StagePlatforms, http.MethodPost, "https://api.igdb.com/v4/platforms", strings.NewReader("fields id; limit 1;"), map[string]string{"Content-Type": "text/plain", "Authorization": "Bearer " + token.AccessToken, "Client-ID": s.IGDBClientID}, &platforms); err != nil || len(platforms) == 0 {
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
	token, err := c.secret(ctx, secrets.TMDBReadToken)
	if err != nil {
		return setup.CheckUnavailable, errCheck
	}
	if s.TMDBTokenEnv != setup.TMDBTokenEnv || token == "" {
		return setup.CheckEnvironment, nil
	}
	var result struct {
		Images struct {
			SecureBaseURL string `json:"secure_base_url"`
		} `json:"images"`
	}
	err = c.request(ctx, http.MethodGet, "https://api.themoviedb.org/3/configuration", nil, map[string]string{"Authorization": "Bearer " + token}, &result)
	if errors.Is(err, errAuthentication) {
		return setup.CheckAuthentication, nil
	}
	if err != nil || !setup.ValidServiceURL(result.Images.SecureBaseURL, false) {
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
	return c.requestStage(ctx, devlogs.StageRequest, method, endpoint, body, headers, result)
}

func (c *Checker) requestStage(ctx context.Context, stage devlogs.Stage, method, endpoint string, body io.Reader, headers map[string]string, result any) error {
	// Only the exact configured issuer discovery URL and configured public origin
	// receive the operator-trusted private-address exception at the dial boundary.
	trusted := endpoint == strings.TrimRight(c.config.Runtime.OIDCIssuer, "/")+"/.well-known/openid-configuration" || sameOrigin(endpoint, c.config.Runtime.PublicURL)
	return c.fetch(ctx, fetchSpec{stage: stage, method: method, endpoint: endpoint, body: body, headers: headers, accept: "application/json",
		trusted: trusted, types: []string{"application/json"}, maxBytes: maxResponse,
		validate: func(data []byte) bool { return json.Unmarshal(data, result) == nil }})
}
