package config

import (
	"log/slog"
	"strings"
	"testing"
)

func TestFromEnvDefaults(t *testing.T) {
	t.Setenv("REQUESTARR_ENV", "")
	t.Setenv("REQUESTARR_HTTP_ADDR", "")
	t.Setenv("REQUESTARR_PUBLIC_URL", "")
	t.Setenv("REQUESTARR_LOG_LEVEL", "")
	t.Setenv("REQUESTARR_DATABASE_URL", "postgres://requestarr:requestarr@localhost:5432/requestarr?sslmode=disable")
	t.Setenv("REQUESTARR_OIDC_ISSUER", "https://id.example.test")
	t.Setenv("REQUESTARR_OIDC_CLIENT_ID", "requestarr")
	t.Setenv("REQUESTARR_OIDC_CLIENT_SECRET", "secret")
	t.Setenv("REQUESTARR_SESSION_HMAC_SECRET", strings.Repeat("x", 32))
	t.Setenv("REQUESTARR_TMDB_READ_TOKEN", "tmdb-token")
	t.Setenv("REQUESTARR_IGDB_CLIENT_ID", "igdb-client")
	t.Setenv("REQUESTARR_IGDB_CLIENT_SECRET", "igdb-secret")
	t.Setenv("REQUESTARR_NAVIDROME_URL", "")
	t.Setenv("REQUESTARR_NAVIDROME_USERNAME", "")
	t.Setenv("REQUESTARR_NAVIDROME_PASSWORD", "")

	got := FromEnv()
	want := Config{
		Environment: "development",
		HTTPAddr:    ":8080",
		PublicURL:   "http://localhost:8080",
		LogLevelRaw: "info",
		DatabaseURL: "postgres://requestarr:requestarr@localhost:5432/requestarr?sslmode=disable",
		OIDCIssuer:  "https://id.example.test", OIDCClientID: "requestarr", OIDCClientSecret: "secret", SessionHMACSecret: strings.Repeat("x", 32),
		TMDBReadToken: "tmdb-token", IGDBClientID: "igdb-client", IGDBClientSecret: "igdb-secret",
	}

	if got != want {
		t.Fatalf("FromEnv() = %#v, want %#v", got, want)
	}
	if err := got.Validate(); err != nil {
		t.Fatalf("default configuration failed validation: %v", err)
	}
}

func TestFromEnvDatabaseURLHasNoDefault(t *testing.T) {
	t.Setenv("REQUESTARR_DATABASE_URL", "")

	got := FromEnv()
	if got.DatabaseURL != "" {
		t.Fatalf("DatabaseURL = %q, want empty when unset", got.DatabaseURL)
	}
	if err := got.Validate(); err == nil {
		t.Fatal("Validate() = nil, want error for missing database url")
	}
}

const validDBURL = "postgres://requestarr:requestarr@localhost:5432/requestarr?sslmode=disable"

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name       string
		config     Config
		wantErr    string
		notInError string
	}{
		{
			name:   "defaults",
			config: Config{HTTPAddr: ":8080", LogLevelRaw: "info", DatabaseURL: validDBURL, PublicURL: "http://localhost:8080", OIDCIssuer: "https://id.example.test", OIDCClientID: "x", OIDCClientSecret: "y", SessionHMACSecret: strings.Repeat("s", 32)},
		},
		{
			name:   "hostname and uppercase log level",
			config: Config{HTTPAddr: "localhost:8080", LogLevelRaw: "DEBUG", DatabaseURL: validDBURL, PublicURL: "http://localhost:8080", OIDCIssuer: "https://id.example.test", OIDCClientID: "x", OIDCClientSecret: "y", SessionHMACSecret: strings.Repeat("s", 32)},
		},
		{
			name:   "IPv4 address",
			config: Config{HTTPAddr: "127.0.0.1:8080", LogLevelRaw: "warn", DatabaseURL: validDBURL, PublicURL: "http://localhost:8080", OIDCIssuer: "https://id.example.test", OIDCClientID: "x", OIDCClientSecret: "y", SessionHMACSecret: strings.Repeat("s", 32)},
		},
		{
			name:   "IPv6 address",
			config: Config{HTTPAddr: "[::1]:8080", LogLevelRaw: "warning", DatabaseURL: validDBURL, PublicURL: "http://localhost:8080", OIDCIssuer: "https://id.example.test", OIDCClientID: "x", OIDCClientSecret: "y", SessionHMACSecret: strings.Repeat("s", 32)},
		},
		{
			name:   "highest valid port",
			config: Config{HTTPAddr: ":65535", LogLevelRaw: "error", DatabaseURL: validDBURL, PublicURL: "http://localhost:8080", OIDCIssuer: "https://id.example.test", OIDCClientID: "x", OIDCClientSecret: "y", SessionHMACSecret: strings.Repeat("s", 32)},
		},
		{
			name:    "missing port",
			config:  Config{HTTPAddr: "localhost", LogLevelRaw: "info", DatabaseURL: validDBURL, PublicURL: "https://example.test", OIDCIssuer: "https://id.example.test", OIDCClientID: "x", OIDCClientSecret: "y", SessionHMACSecret: strings.Repeat("s", 32)},
			wantErr: "REQUESTARR_HTTP_ADDR must be in host:port form",
		},
		{
			name:    "non-numeric port",
			config:  Config{HTTPAddr: "localhost:http", LogLevelRaw: "info", DatabaseURL: validDBURL, PublicURL: "https://example.test", OIDCIssuer: "https://id.example.test", OIDCClientID: "x", OIDCClientSecret: "y", SessionHMACSecret: strings.Repeat("s", 32)},
			wantErr: "REQUESTARR_HTTP_ADDR port must be a number between 1 and 65535",
		},
		{
			name:    "zero port",
			config:  Config{HTTPAddr: ":0", LogLevelRaw: "info", DatabaseURL: validDBURL, PublicURL: "https://example.test", OIDCIssuer: "https://id.example.test", OIDCClientID: "x", OIDCClientSecret: "y", SessionHMACSecret: strings.Repeat("s", 32)},
			wantErr: "REQUESTARR_HTTP_ADDR port must be a number between 1 and 65535",
		},
		{
			name:    "port above maximum",
			config:  Config{HTTPAddr: ":65536", LogLevelRaw: "info", DatabaseURL: validDBURL, PublicURL: "https://example.test", OIDCIssuer: "https://id.example.test", OIDCClientID: "x", OIDCClientSecret: "y", SessionHMACSecret: strings.Repeat("s", 32)},
			wantErr: "REQUESTARR_HTTP_ADDR port must be a number between 1 and 65535",
		},
		{
			name:       "invalid log level does not expose value",
			config:     Config{HTTPAddr: ":8080", LogLevelRaw: "secret-log-value", DatabaseURL: validDBURL, PublicURL: "https://example.test", OIDCIssuer: "https://id.example.test", OIDCClientID: "x", OIDCClientSecret: "y", SessionHMACSecret: strings.Repeat("s", 32)},
			wantErr:    "REQUESTARR_LOG_LEVEL must be one of debug, info, warn, warning, or error",
			notInError: "secret-log-value",
		},
		{
			name:    "multiple invalid settings",
			config:  Config{HTTPAddr: "bad-address", LogLevelRaw: "trace", DatabaseURL: validDBURL, PublicURL: "https://example.test", OIDCIssuer: "https://id.example.test", OIDCClientID: "x", OIDCClientSecret: "y", SessionHMACSecret: strings.Repeat("s", 32)},
			wantErr: "REQUESTARR_HTTP_ADDR must be in host:port form; REQUESTARR_LOG_LEVEL must be one of debug, info, warn, warning, or error",
		},
		{
			name:    "missing database url",
			config:  Config{HTTPAddr: ":8080", LogLevelRaw: "info"},
			wantErr: "database is not configured: set REQUESTARR_DATABASE_URL, or set REQUESTARR_DB_HOST, REQUESTARR_DB_NAME, REQUESTARR_DB_USER and REQUESTARR_DB_PASSWORD; REQUESTARR_PUBLIC_URL must be an absolute http(s) origin without path, credentials, query, or fragment; REQUESTARR_SESSION_HMAC_SECRET must contain at least 32 bytes",
		},
		{
			name:    "missing database url does not expose other problems' order dependency",
			config:  Config{HTTPAddr: "bad-address", LogLevelRaw: "info"},
			wantErr: "database is not configured: set REQUESTARR_DATABASE_URL, or set REQUESTARR_DB_HOST, REQUESTARR_DB_NAME, REQUESTARR_DB_USER and REQUESTARR_DB_PASSWORD; REQUESTARR_PUBLIC_URL must be an absolute http(s) origin without path, credentials, query, or fragment; REQUESTARR_SESSION_HMAC_SECRET must contain at least 32 bytes; REQUESTARR_HTTP_ADDR must be in host:port form",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() returned unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() returned nil, want %q", tt.wantErr)
			}
			if err.Error() != tt.wantErr {
				t.Fatalf("Validate() error = %q, want %q", err, tt.wantErr)
			}
			if tt.notInError != "" && strings.Contains(err.Error(), tt.notInError) {
				t.Fatalf("Validate() error exposed configured value %q", tt.notInError)
			}
		})
	}
}

func TestConfigLogLevel(t *testing.T) {
	tests := []struct {
		input string
		want  slog.Level
	}{
		{input: "debug", want: slog.LevelDebug},
		{input: "INFO", want: slog.LevelInfo},
		{input: "warn", want: slog.LevelWarn},
		{input: "warning", want: slog.LevelWarn},
		{input: "error", want: slog.LevelError},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			cfg := Config{LogLevelRaw: tt.input}
			if got := cfg.LogLevel(); got != tt.want {
				t.Fatalf("LogLevel() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConfigAllowsOptionalOIDCAndRequiresSecurePublicOrigin(t *testing.T) {
	base := Config{Environment: "production", HTTPAddr: ":8080", LogLevelRaw: "info", DatabaseURL: validDBURL, PublicURL: "http://app.example", OIDCIssuer: "https://id.example", OIDCClientID: "client", OIDCClientSecret: "secret", SessionHMACSecret: strings.Repeat("s", 32)}
	if err := base.Validate(); err == nil || !strings.Contains(err.Error(), "must use https outside development") {
		t.Fatalf("Validate()=%v, want production HTTPS error", err)
	}
	base.PublicURL = "https://app.example/path"
	if err := base.Validate(); err == nil || !strings.Contains(err.Error(), "without path") {
		t.Fatalf("Validate()=%v, want path rejection", err)
	}
	base.PublicURL = "https://app.example"
	base.OIDCClientSecret = ""
	if err := base.Validate(); err != nil {
		t.Fatal("partial OIDC must leave local recovery available")
	}
}

func TestAppriseConfigurationMustBePairedAndSecure(t *testing.T) {
	base := Config{Environment: "production", HTTPAddr: ":8080", LogLevelRaw: "info", DatabaseURL: validDBURL, PublicURL: "https://app.example", OIDCIssuer: "https://id.example", OIDCClientID: "client", OIDCClientSecret: "secret", SessionHMACSecret: strings.Repeat("s", 32)}
	base.AppriseURL = "https://apprise.example"
	if err := base.Validate(); err == nil || !strings.Contains(err.Error(), "must be configured together") {
		t.Fatalf("Validate()=%v, want pair validation", err)
	}
	base.AppriseConfigKey = "key"
	if err := base.Validate(); err != nil {
		t.Fatalf("valid Apprise pair rejected: %v", err)
	}
	base.AppriseURL = "http://apprise.example"
	if err := base.Validate(); err == nil || !strings.Contains(err.Error(), "must be an absolute https URL") {
		t.Fatalf("Validate()=%v, want HTTPS enforcement", err)
	}
}

func validBase() Config {
	return Config{Environment: "development", HTTPAddr: ":8080", LogLevelRaw: "info", DatabaseURL: "postgres://x", OIDCIssuer: "https://id.example.test", OIDCClientID: "c", OIDCClientSecret: "s", SessionHMACSecret: strings.Repeat("x", 32), PublicURL: "http://localhost:8080"}
}

func TestPlexConfigurationMustBeCompleteAndSafe(t *testing.T) {
	t.Setenv("REQUESTARR_PLEX_URL", "http://plex.local:32400")
	t.Setenv("REQUESTARR_PLEX_TOKEN", "tok")
	got := FromEnv()
	if got.PlexURL != "http://plex.local:32400" || got.PlexToken != "tok" {
		t.Fatalf("FromEnv plex = %q %q", got.PlexURL, got.PlexToken)
	}
	for name, tc := range map[string]struct {
		env      string
		url, tok string
		wantErr  string
	}{
		"unset":              {"development", "", "", ""},
		"complete":           {"development", "http://plex.local:32400", "tok", ""},
		"token only":         {"development", "", "tok", "REQUESTARR_PLEX_URL and REQUESTARR_PLEX_TOKEN must be configured together"},
		"url only":           {"development", "http://plex.local:32400", "", "REQUESTARR_PLEX_URL and REQUESTARR_PLEX_TOKEN must be configured together"},
		"credentials in url": {"development", "http://u:p@plex.local:32400", "tok", "REQUESTARR_PLEX_URL must be an absolute http(s) origin"},
		"path in url":        {"development", "http://plex.local:32400/web", "tok", "REQUESTARR_PLEX_URL must be an absolute http(s) origin"},
		"http in production": {"production", "http://plex.example.test:32400", "tok", "REQUESTARR_PLEX_URL must use https outside development"},
		"https production":   {"production", "https://plex.example", "tok", ""},
	} {
		t.Run(name, func(t *testing.T) {
			c := validBase()
			c.Environment, c.PlexURL, c.PlexToken = tc.env, tc.url, tc.tok
			if tc.env == "production" {
				c.PublicURL = "https://requests.example.test"
			}
			err := c.Validate()
			switch {
			case tc.wantErr == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
				t.Fatalf("error %v does not contain %q", err, tc.wantErr)
			}
		})
	}
}

func TestNavidromeConfigurationMustBeCompleteAndSafe(t *testing.T) {
	base := validBase()
	t.Setenv("REQUESTARR_NAVIDROME_URL", "https://music.example.test")
	t.Setenv("REQUESTARR_NAVIDROME_USERNAME", "requestarr")
	t.Setenv("REQUESTARR_NAVIDROME_PASSWORD", "server-secret")
	got := FromEnv()
	if got.NavidromeURL != "https://music.example.test" || got.NavidromeUsername != "requestarr" || got.NavidromePassword != "server-secret" {
		t.Fatalf("Navidrome env config not loaded: %#v", got)
	}

	for name, tc := range map[string]struct {
		url, username, password string
		environment             string
		wantErr                 string
	}{
		"unset":                  {environment: "development"},
		"complete":               {url: "http://music.local:4533", username: "requestarr", password: "secret", environment: "development"},
		"missing password":       {url: "https://music.example.test", username: "requestarr", environment: "production", wantErr: "must be configured together"},
		"credentials in URL":     {url: "https://user:secret@music.example.test", username: "requestarr", password: "secret", environment: "production", wantErr: "REQUESTARR_NAVIDROME_URL must be an absolute http(s) origin"},
		"path in URL":            {url: "https://music.example.test/navidrome", username: "requestarr", password: "secret", environment: "production", wantErr: "REQUESTARR_NAVIDROME_URL must be an absolute http(s) origin"},
		"HTTP in production":     {url: "http://music.example.test", username: "requestarr", password: "secret", environment: "production", wantErr: "REQUESTARR_NAVIDROME_URL must use https outside development"},
		"valid HTTPS production": {url: "https://music.example.test", username: "requestarr", password: "secret", environment: "production"},
	} {
		t.Run(name, func(t *testing.T) {
			c := base
			c.Environment = tc.environment
			c.NavidromeURL, c.NavidromeUsername, c.NavidromePassword = tc.url, tc.username, tc.password
			if tc.environment == "production" {
				c.PublicURL = "https://requests.example.test"
			}
			err := c.Validate()
			if tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)) {
				t.Fatalf("error %v does not contain %q", err, tc.wantErr)
			}
			if tc.wantErr == "" && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestRommConfigurationMustBeCompleteAndSafe(t *testing.T) {
	t.Setenv("REQUESTARR_ROMM_URL", "http://romm.local:3000")
	t.Setenv("REQUESTARR_ROMM_TOKEN", "tok")
	if got := FromEnv(); got.RommURL != "http://romm.local:3000" || got.RommToken != "tok" {
		t.Fatalf("FromEnv romm = %q %q", got.RommURL, got.RommToken)
	}
	for name, tc := range map[string]struct {
		env, url, tok, wantErr string
	}{
		"unset":              {"development", "", "", ""},
		"complete":           {"development", "http://romm.local:3000", "tok", ""},
		"token only":         {"development", "", "tok", "REQUESTARR_ROMM_URL and REQUESTARR_ROMM_TOKEN must be configured together"},
		"url only":           {"development", "http://romm.local:3000", "", "REQUESTARR_ROMM_URL and REQUESTARR_ROMM_TOKEN must be configured together"},
		"path in url":        {"development", "http://romm.local:3000/api", "tok", "REQUESTARR_ROMM_URL must be an absolute http(s) origin"},
		"http in production": {"production", "http://romm.example.test:3000", "tok", "REQUESTARR_ROMM_URL must use https outside development"},
	} {
		t.Run(name, func(t *testing.T) {
			c := validBase()
			c.Environment, c.RommURL, c.RommToken = tc.env, tc.url, tc.tok
			if tc.env == "production" {
				c.PublicURL = "https://requests.example.test"
			}
			err := c.Validate()
			switch {
			case tc.wantErr == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
				t.Fatalf("error %v does not contain %q", err, tc.wantErr)
			}
		})
	}
}

func TestOIDCIssuerMayUseLoopbackHTTPOnlyInDevelopment(t *testing.T) {
	for _, tc := range []struct {
		env, issuer string
		ok          bool
	}{
		{"development", "http://localhost:8081/realms/requestarr", true},
		{"development", "http://127.0.0.1:8081/realms/requestarr", true},
		{"development", "http://id.example.test/realms/requestarr", false},
		{"production", "http://localhost:8081/realms/requestarr", false},
		{"development", "https://id.example.test", true},
	} {
		cfg := validBase()
		cfg.Environment, cfg.OIDCIssuer = tc.env, tc.issuer
		if err := cfg.Validate(); (err == nil) != tc.ok {
			t.Errorf("%s %s: Validate()=%v, want ok=%v", tc.env, tc.issuer, err, tc.ok)
		}
	}
}
