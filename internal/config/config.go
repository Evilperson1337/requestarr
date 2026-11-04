package config

import (
	"errors"
	"log/slog"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/secrets"
	"github.com/evilperson1337/requestarr/internal/sources"
)

type Config struct {
	DevelopmentLogs   bool
	devLogsInvalid    bool
	Environment       string
	HTTPAddr          string
	PublicURL         string
	LogLevelRaw       string
	DatabaseURL       string
	OIDCIssuer        string
	OIDCClientID      string
	OIDCClientSecret  string
	SessionHMACSecret string
	TMDBReadToken     string
	IGDBClientID      string
	IGDBClientSecret  string
	// AudibleRegion is the marketplace assumed for a bare ASIN (D031); "us" when unset.
	AudibleRegion        string
	JellyfinURL          string
	JellyfinAPIKey       string
	PlexURL              string
	PlexToken            string
	RommURL              string
	RommToken            string
	NavidromeURL         string
	NavidromeUsername    string
	NavidromePassword    string
	AudiobookshelfURL    string
	AudiobookshelfAPIKey string
	AppriseURL           string
	AppriseConfigKey     string
	// SecretsKey is the master key for stored secrets (D024). Optional.
	SecretsKey string
	// HTTPAllowedNetworks extends the built-in private-host list for library
	// Source URLs (D033): comma-separated CIDRs and hostname suffixes.
	HTTPAllowedNetworks string
	// parsedNetworks is HTTPAllowedNetworks parsed once by
	// SetHTTPAllowedNetworks; nil when the setting is empty.
	parsedNetworks *parsedNetworks
}

func FromEnv() Config {
	devLogs, devLogsErr := strconv.ParseBool(env("REQUESTARR_DEV_LOGS", "false"))
	c := Config{
		DevelopmentLogs:   devLogs,
		devLogsInvalid:    devLogsErr != nil,
		Environment:       env("REQUESTARR_ENV", "development"),
		HTTPAddr:          env("REQUESTARR_HTTP_ADDR", ":8080"),
		PublicURL:         env("REQUESTARR_PUBLIC_URL", "http://localhost:8080"),
		LogLevelRaw:       env("REQUESTARR_LOG_LEVEL", "info"),
		DatabaseURL:       env("REQUESTARR_DATABASE_URL", ""),
		OIDCIssuer:        os.Getenv("REQUESTARR_OIDC_ISSUER"),
		OIDCClientID:      os.Getenv("REQUESTARR_OIDC_CLIENT_ID"),
		OIDCClientSecret:  os.Getenv("REQUESTARR_OIDC_CLIENT_SECRET"),
		SessionHMACSecret: os.Getenv("REQUESTARR_SESSION_HMAC_SECRET"),
		TMDBReadToken:     os.Getenv("REQUESTARR_TMDB_READ_TOKEN"),
		IGDBClientID:      os.Getenv("REQUESTARR_IGDB_CLIENT_ID"),
		IGDBClientSecret:  os.Getenv("REQUESTARR_IGDB_CLIENT_SECRET"),
		AudibleRegion:     strings.ToLower(strings.TrimSpace(os.Getenv("REQUESTARR_AUDIBLE_REGION"))),
		AppriseURL:        os.Getenv("REQUESTARR_APPRISE_URL"),
		AppriseConfigKey:  os.Getenv("REQUESTARR_APPRISE_CONFIG_KEY"),
		SecretsKey:        os.Getenv(secrets.MasterKeyEnv),
	}
	c.SetHTTPAllowedNetworks(os.Getenv(httpAllowedNetworksEnv))
	for _, kind := range sources.All() {
		*c.LibraryURL(kind.Slug) = os.Getenv(kind.URLEnv)
		for _, credential := range kind.Credentials {
			*c.LibraryCredential(kind.Slug, credential.Role) = os.Getenv(credential.Env)
		}
	}
	return c
}

// libraryRefs points at one Source kind's Config fields. A nil field means the
// kind has no such credential.
type libraryRefs struct{ URL, Token, Username, Password *string }

// libraryFields is the one place the per-kind Config fields are named.
func (c *Config) libraryFields(slug string) libraryRefs {
	switch slug {
	case "jellyfin":
		return libraryRefs{URL: &c.JellyfinURL, Token: &c.JellyfinAPIKey}
	case "plex":
		return libraryRefs{URL: &c.PlexURL, Token: &c.PlexToken}
	case "romm":
		return libraryRefs{URL: &c.RommURL, Token: &c.RommToken}
	case "navidrome":
		return libraryRefs{URL: &c.NavidromeURL, Username: &c.NavidromeUsername, Password: &c.NavidromePassword}
	case "audiobookshelf":
		return libraryRefs{URL: &c.AudiobookshelfURL, Token: &c.AudiobookshelfAPIKey}
	}
	panic("config: unknown Source kind " + slug)
}

// LibraryURL returns the configured URL field of a Source kind. It panics on a
// slug that is not in the sources registry, which is a programming error.
func (c *Config) LibraryURL(slug string) *string { return c.libraryFields(slug).URL }

// LibraryCredential returns the configured value of a Source kind's credential
// field. It panics when the kind has no such field.
func (c *Config) LibraryCredential(slug string, role sources.Role) *string {
	refs := c.libraryFields(slug)
	var field *string
	switch role {
	case sources.RoleToken:
		field = refs.Token
	case sources.RoleUsername:
		field = refs.Username
	case sources.RolePassword:
		field = refs.Password
	}
	if field == nil {
		panic("config: " + slug + " has no " + string(role) + " credential")
	}
	return field
}

// LibraryProblems is librarySourceProblems for one Source slug, so a single
// Source can be checked (and applied) without validating the rest.
func (c Config) LibraryProblems(slug string) []string {
	kind, ok := sources.Lookup(slug)
	if !ok {
		return []string{"unknown library source"}
	}
	return c.librarySourceProblems(kind)
}

// librarySourceProblems enforces that a Source's URL and every Credential field
// are configured together, and that a configured URL is a valid origin.
func (c Config) librarySourceProblems(kind sources.Kind) []string {
	url, set := *c.LibraryURL(kind.Slug), *c.LibraryURL(kind.Slug) != ""
	names := []string{kind.URLEnv}
	for _, credential := range kind.Credentials {
		names = append(names, credential.Env)
		if *c.LibraryCredential(kind.Slug, credential.Role) != "" {
			set = true
		}
	}
	if !set {
		return nil
	}
	complete := url != ""
	for _, credential := range kind.Credentials {
		complete = complete && *c.LibraryCredential(kind.Slug, credential.Role) != ""
	}
	if !complete {
		return []string{joinNames(names) + " must be configured together"}
	}
	return c.sourceURLProblems(kind.URLEnv, url)
}

// joinNames renders "A and B" or "A, B, and C".
func joinNames(names []string) string {
	if len(names) == 2 {
		return names[0] + " and " + names[1]
	}
	return strings.Join(names[:len(names)-1], ", ") + ", and " + names[len(names)-1]
}

// ValidateBootstrap checks only what is needed to open the database and read
// stored secrets. The full Validate runs afterwards, once stored secrets have
// been resolved into the Config (D024).
func (c Config) ValidateBootstrap() error {
	var problems []string
	if c.DatabaseURL == "" {
		problems = append(problems, "REQUESTARR_DATABASE_URL is required")
	}
	problems = append(problems, c.secretsKeyProblems()...)
	problems = append(problems, c.httpAllowedNetworksProblems()...)
	if c.AudibleRegion != "" && !domain.AudibleRegion(c.AudibleRegion).Valid() {
		problems = append(problems, "REQUESTARR_AUDIBLE_REGION must be one of us, ca, uk, au, fr, de, jp, it, in, es")
	}
	problems = append(problems, c.developmentLogsProblems()...)
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

func (c Config) developmentLogsProblems() []string {
	if c.devLogsInvalid {
		return []string{"REQUESTARR_DEV_LOGS must be a boolean"}
	}
	if c.DevelopmentLogs && c.Environment != "development" {
		return []string{"REQUESTARR_DEV_LOGS is only permitted in development"}
	}
	return nil
}

func (c Config) secretsKeyProblems() []string {
	if c.SecretsKey == "" {
		return nil
	}
	if _, err := secrets.ParseMasterKey(c.SecretsKey); err != nil {
		return []string{err.Error()}
	}
	return nil
}

// sourceURLProblems validates an operator-configured library Source URL: an
// origin without credentials, path, query or fragment, over https outside development.
func (c Config) sourceURLProblems(name, raw string) []string {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return []string{name + " must be an absolute http(s) origin without credentials, path, query, or fragment"}
	}
	if c.Environment != "" && c.Environment != "development" && u.Scheme != "https" {
		if !c.PrivateHosts().Contains(u.Hostname()) {
			return []string{name + " must use https outside development, unless the host is on a private network (use https, or add the network to " + httpAllowedNetworksEnv + ")"}
		}
	}
	return nil
}

func (c Config) Validate() error {
	var problems []string
	problems = append(problems, c.developmentLogsProblems()...)

	if c.DatabaseURL == "" {
		problems = append(problems, "REQUESTARR_DATABASE_URL is required")
	}
	problems = append(problems, c.secretsKeyProblems()...)
	problems = append(problems, c.httpAllowedNetworksProblems()...)
	for _, kind := range sources.All() {
		problems = append(problems, c.librarySourceProblems(kind)...)
	}
	if (c.AppriseURL == "") != (c.AppriseConfigKey == "") {
		problems = append(problems, "REQUESTARR_APPRISE_URL and REQUESTARR_APPRISE_CONFIG_KEY must be configured together")
	} else if c.AppriseURL != "" {
		u, err := url.Parse(c.AppriseURL)
		if err != nil || (u.Scheme != "https" && !(c.Environment == "development" && u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1"))) || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			problems = append(problems, "REQUESTARR_APPRISE_URL must be an absolute https URL (or local http in development), without credentials, query, or fragment")
		}
		if len(c.AppriseConfigKey) > 256 || strings.TrimSpace(c.AppriseConfigKey) != c.AppriseConfigKey {
			problems = append(problems, "REQUESTARR_APPRISE_CONFIG_KEY is invalid")
		}
	}
	if c.PublicURL == "" || !validPublicURL(c.PublicURL) {
		problems = append(problems, "REQUESTARR_PUBLIC_URL must be an absolute http(s) origin without path, credentials, query, or fragment")
	}
	if c.Environment == "" || c.Environment == "development" {
		// Empty Environment is retained for direct Config construction in tests;
		// FromEnv always assigns an explicit environment.
	} else if strings.HasPrefix(c.PublicURL, "https://") {
		// HTTPS is required outside development.
	} else {
		problems = append(problems, "REQUESTARR_PUBLIC_URL must use https outside development")
	}
	{
		if c.OIDCIssuer != "" {
			u, err := url.Parse(c.OIDCIssuer)
			if err != nil || !(u.Scheme == "https" || (c.Environment == "development" && u.Scheme == "http" && isLoopbackHost(u.Hostname()))) || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
				problems = append(problems, "REQUESTARR_OIDC_ISSUER must be an absolute https URL (http only for localhost in development) without credentials, query, or fragment")
			}
		}
		if c.OIDCIssuer == "" || c.OIDCClientID == "" || c.OIDCClientSecret == "" {
			problems = append(problems, "REQUESTARR_OIDC_ISSUER, REQUESTARR_OIDC_CLIENT_ID, and REQUESTARR_OIDC_CLIENT_SECRET are required")
		}
		if len(c.SessionHMACSecret) < 32 {
			problems = append(problems, "REQUESTARR_SESSION_HMAC_SECRET must contain at least 32 bytes")
		}
	}

	_, port, err := net.SplitHostPort(c.HTTPAddr)
	if err != nil {
		problems = append(problems, "REQUESTARR_HTTP_ADDR must be in host:port form")
	} else if portNumber, err := strconv.Atoi(port); err != nil || portNumber < 1 || portNumber > 65535 {
		problems = append(problems, "REQUESTARR_HTTP_ADDR port must be a number between 1 and 65535")
	}

	switch strings.ToLower(c.LogLevelRaw) {
	case "debug", "info", "warn", "warning", "error":
	default:
		problems = append(problems, "REQUESTARR_LOG_LEVEL must be one of debug, info, warn, warning, or error")
	}

	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

func validPublicURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && u.User == nil && (u.Path == "" || u.Path == "/") && u.RawQuery == "" && u.Fragment == ""
}

func (c Config) LogLevel() slog.Level {
	switch strings.ToLower(c.LogLevelRaw) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

// isLoopbackHost reports names that only reach the local machine; plain HTTP is
// tolerated for them in development.
func isLoopbackHost(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}
