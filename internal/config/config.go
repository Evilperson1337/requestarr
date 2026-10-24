package config

import (
	"errors"
	"log/slog"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type Config struct {
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
	JellyfinURL       string
	JellyfinAPIKey    string
	PlexURL           string
	PlexToken         string
	RommURL           string
	RommToken         string
	NavidromeURL      string
	NavidromeUsername string
	NavidromePassword string
	AppriseURL        string
	AppriseConfigKey  string
}

func FromEnv() Config {
	return Config{
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
		JellyfinURL:       os.Getenv("REQUESTARR_JELLYFIN_URL"),
		JellyfinAPIKey:    os.Getenv("REQUESTARR_JELLYFIN_API_KEY"),
		PlexURL:           os.Getenv("REQUESTARR_PLEX_URL"),
		PlexToken:         os.Getenv("REQUESTARR_PLEX_TOKEN"),
		RommURL:           os.Getenv("REQUESTARR_ROMM_URL"),
		RommToken:         os.Getenv("REQUESTARR_ROMM_TOKEN"),
		NavidromeURL:      os.Getenv("REQUESTARR_NAVIDROME_URL"),
		NavidromeUsername: os.Getenv("REQUESTARR_NAVIDROME_USERNAME"),
		NavidromePassword: os.Getenv("REQUESTARR_NAVIDROME_PASSWORD"),
		AppriseURL:        os.Getenv("REQUESTARR_APPRISE_URL"),
		AppriseConfigKey:  os.Getenv("REQUESTARR_APPRISE_CONFIG_KEY"),
	}
}

// sourceURLProblems validates an operator-configured library Source URL: an
// origin without credentials, path, query or fragment, over https outside development.
func (c Config) sourceURLProblems(name, raw string) []string {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return []string{name + " must be an absolute http(s) origin without credentials, path, query, or fragment"}
	}
	if c.Environment != "" && c.Environment != "development" && u.Scheme != "https" {
		return []string{name + " must use https outside development"}
	}
	return nil
}

func (c Config) Validate() error {
	var problems []string

	if c.DatabaseURL == "" {
		problems = append(problems, "REQUESTARR_DATABASE_URL is required")
	}
	if (c.JellyfinURL == "") != (c.JellyfinAPIKey == "") {
		problems = append(problems, "REQUESTARR_JELLYFIN_URL and REQUESTARR_JELLYFIN_API_KEY must be configured together")
	} else if c.JellyfinURL != "" {
		problems = append(problems, c.sourceURLProblems("REQUESTARR_JELLYFIN_URL", c.JellyfinURL)...)
	}
	if (c.PlexURL == "") != (c.PlexToken == "") {
		problems = append(problems, "REQUESTARR_PLEX_URL and REQUESTARR_PLEX_TOKEN must be configured together")
	} else if c.PlexURL != "" {
		problems = append(problems, c.sourceURLProblems("REQUESTARR_PLEX_URL", c.PlexURL)...)
	}
	if (c.RommURL == "") != (c.RommToken == "") {
		problems = append(problems, "REQUESTARR_ROMM_URL and REQUESTARR_ROMM_TOKEN must be configured together")
	} else if c.RommURL != "" {
		problems = append(problems, c.sourceURLProblems("REQUESTARR_ROMM_URL", c.RommURL)...)
	}
	navidromeConfigured := c.NavidromeURL != "" || c.NavidromeUsername != "" || c.NavidromePassword != ""
	if navidromeConfigured {
		if c.NavidromeURL == "" || c.NavidromeUsername == "" || c.NavidromePassword == "" {
			problems = append(problems, "REQUESTARR_NAVIDROME_URL, REQUESTARR_NAVIDROME_USERNAME, and REQUESTARR_NAVIDROME_PASSWORD must be configured together")
		} else {
			problems = append(problems, c.sourceURLProblems("REQUESTARR_NAVIDROME_URL", c.NavidromeURL)...)
		}
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
