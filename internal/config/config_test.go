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
			wantErr: "REQUESTARR_DATABASE_URL is required; REQUESTARR_PUBLIC_URL must be an absolute http(s) origin without path, credentials, query, or fragment; REQUESTARR_OIDC_ISSUER, REQUESTARR_OIDC_CLIENT_ID, and REQUESTARR_OIDC_CLIENT_SECRET are required; REQUESTARR_SESSION_HMAC_SECRET must contain at least 32 bytes",
		},
		{
			name:    "missing database url does not expose other problems' order dependency",
			config:  Config{HTTPAddr: "bad-address", LogLevelRaw: "info"},
			wantErr: "REQUESTARR_DATABASE_URL is required; REQUESTARR_PUBLIC_URL must be an absolute http(s) origin without path, credentials, query, or fragment; REQUESTARR_OIDC_ISSUER, REQUESTARR_OIDC_CLIENT_ID, and REQUESTARR_OIDC_CLIENT_SECRET are required; REQUESTARR_SESSION_HMAC_SECRET must contain at least 32 bytes; REQUESTARR_HTTP_ADDR must be in host:port form",
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

func TestConfigRequiresOIDCAndSecurePublicOrigin(t *testing.T) {
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
	if err := base.Validate(); err == nil || !strings.Contains(err.Error(), "OIDC_ISSUER") {
		t.Fatalf("Validate()=%v, want required OIDC config", err)
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
