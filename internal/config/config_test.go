package config

import (
	"encoding/base64"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

func TestFromEnvDefaults(t *testing.T) {
	t.Setenv("REQUESTARR_ENV", "")
	t.Setenv("REQUESTARR_HTTP_ADDR", "")
	t.Setenv("REQUESTARR_PUBLIC_URL", "")
	t.Setenv("REQUESTARR_LOG_LEVEL", "")
	t.Setenv("REQUESTARR_DATABASE_URL", "postgres://requestarr:secret@localhost:5432/requestarr?sslmode=disable")
	t.Setenv("REQUESTARR_SETUP_TOKEN", "")

	got := FromEnv()
	want := Config{
		Environment: "development",
		HTTPAddr:    ":8080",
		PublicURL:   "http://localhost:8080",
		LogLevelRaw: "info",
		databaseURL: "postgres://requestarr:secret@localhost:5432/requestarr?sslmode=disable",
	}

	if got != want {
		t.Fatalf("FromEnv() = %#v, want %#v", got, want)
	}
	if err := got.Validate(); err != nil {
		t.Fatalf("default configuration failed validation: %v", err)
	}
}

func TestConfigValidate(t *testing.T) {
	validDatabaseURL := "postgres://requestarr:secret@localhost:5432/requestarr?sslmode=disable"
	tests := []struct {
		name       string
		config     Config
		wantErr    string
		notInError string
	}{
		{
			name:   "defaults",
			config: Config{Environment: "development", HTTPAddr: ":8080", PublicURL: "http://localhost:8080", LogLevelRaw: "info", databaseURL: validDatabaseURL},
		},
		{
			name:   "hostname and uppercase log level",
			config: Config{Environment: "development", HTTPAddr: "localhost:8080", PublicURL: "https://requestarr.example", LogLevelRaw: "DEBUG", databaseURL: validDatabaseURL},
		},
		{
			name:   "IPv4 address",
			config: Config{Environment: "development", HTTPAddr: "127.0.0.1:8080", PublicURL: "http://127.1.2.3:8080", LogLevelRaw: "warn", databaseURL: validDatabaseURL},
		},
		{
			name:   "IPv6 address",
			config: Config{Environment: "development", HTTPAddr: "[::1]:8080", PublicURL: "http://[::1]:8080", LogLevelRaw: "warning", databaseURL: validDatabaseURL},
		},
		{
			name:   "highest valid port",
			config: Config{Environment: "production", HTTPAddr: ":65535", PublicURL: "https://requestarr.example:8443", LogLevelRaw: "error", databaseURL: validDatabaseURL},
		},
		{
			name:    "missing port",
			config:  Config{Environment: "development", HTTPAddr: "localhost", PublicURL: "http://localhost:8080", LogLevelRaw: "info", databaseURL: validDatabaseURL},
			wantErr: "REQUESTARR_HTTP_ADDR must be in host:port form",
		},
		{
			name:    "non-numeric port",
			config:  Config{Environment: "development", HTTPAddr: "localhost:http", PublicURL: "http://localhost:8080", LogLevelRaw: "info", databaseURL: validDatabaseURL},
			wantErr: "REQUESTARR_HTTP_ADDR port must be a number between 1 and 65535",
		},
		{
			name:    "zero port",
			config:  Config{Environment: "development", HTTPAddr: ":0", PublicURL: "http://localhost:8080", LogLevelRaw: "info", databaseURL: validDatabaseURL},
			wantErr: "REQUESTARR_HTTP_ADDR port must be a number between 1 and 65535",
		},
		{
			name:    "port above maximum",
			config:  Config{Environment: "development", HTTPAddr: ":65536", PublicURL: "http://localhost:8080", LogLevelRaw: "info", databaseURL: validDatabaseURL},
			wantErr: "REQUESTARR_HTTP_ADDR port must be a number between 1 and 65535",
		},
		{
			name:       "invalid log level does not expose value",
			config:     Config{Environment: "development", HTTPAddr: ":8080", PublicURL: "http://localhost:8080", LogLevelRaw: "secret-log-value", databaseURL: validDatabaseURL},
			wantErr:    "REQUESTARR_LOG_LEVEL must be one of debug, info, warn, warning, or error",
			notInError: "secret-log-value",
		},
		{
			name:    "multiple invalid settings",
			config:  Config{Environment: "development", HTTPAddr: "bad-address", PublicURL: "http://localhost:8080", LogLevelRaw: "trace", databaseURL: validDatabaseURL},
			wantErr: "REQUESTARR_HTTP_ADDR must be in host:port form; REQUESTARR_LOG_LEVEL must be one of debug, info, warn, warning, or error",
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

func TestDatabaseURLValidationAndRedaction(t *testing.T) {
	secret := "top-secret-password"
	tests := []struct {
		name    string
		value   string
		wantErr string
	}{
		{name: "missing", wantErr: "REQUESTARR_DATABASE_URL is required"},
		{name: "relative", value: "localhost/requestarr", wantErr: "REQUESTARR_DATABASE_URL must be an absolute PostgreSQL URL"},
		{name: "wrong scheme", value: "mysql://user:" + secret + "@localhost/requestarr", wantErr: "REQUESTARR_DATABASE_URL must use postgres:// or postgresql://"},
		{name: "missing database", value: "postgres://user:" + secret + "@localhost/", wantErr: "REQUESTARR_DATABASE_URL must select a non-empty database name"},
		{name: "query clears database", value: "postgres://user:" + secret + "@localhost/requestarr?database=", wantErr: "REQUESTARR_DATABASE_URL must select a non-empty database name"},
		{name: "encoded slash is not a database", value: "postgres://user:" + secret + "@localhost/%2F", wantErr: "REQUESTARR_DATABASE_URL must select a non-empty database name"},
		{name: "malformed", value: "postgres://user:" + secret + "@localhost/%zz", wantErr: "REQUESTARR_DATABASE_URL must be an absolute PostgreSQL URL"},
		{name: "postgres", value: "postgres://user:" + secret + "@localhost/requestarr?sslmode=disable"},
		{name: "postgresql", value: "postgresql://user:" + secret + "@localhost/requestarr?connect_timeout=3"},
		{name: "query selects database", value: "postgres://user:" + secret + "@localhost/ignored?database=requestarr"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{Environment: "development", HTTPAddr: ":8080", PublicURL: "http://localhost:8080", LogLevelRaw: "info", databaseURL: tt.value}
			err := cfg.Validate()
			if tt.wantErr == "" && err != nil {
				t.Fatalf("Validate() returned unexpected error: %v", err)
			}
			if tt.wantErr != "" && (err == nil || err.Error() != tt.wantErr) {
				t.Fatalf("Validate() error = %v, want %q", err, tt.wantErr)
			}
			for _, rendered := range []string{fmt.Sprint(cfg), fmt.Sprintf("%#v", cfg)} {
				if strings.Contains(rendered, secret) || strings.Contains(rendered, tt.value) && tt.value != "" {
					t.Fatalf("configuration rendering exposed database URL: %s", rendered)
				}
			}
			if err != nil && (strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), tt.value) && tt.value != "") {
				t.Fatalf("validation error exposed database URL: %v", err)
			}
		})
	}
}

func TestSetupTokenCanonicalParsingAndRedaction(t *testing.T) {
	validBytes := [32]byte{}
	for i := range validBytes {
		validBytes[i] = byte(i + 1)
	}
	valid := base64.RawURLEncoding.EncodeToString(validBytes[:])
	tests := []struct {
		name  string
		value string
		ready bool
	}{
		{name: "missing"},
		{name: "valid canonical", value: valid, ready: true},
		{name: "padded", value: valid + "="},
		{name: "hex", value: strings.Repeat("a", 64)},
		{name: "UUID", value: "00000000-0000-4000-8000-000000000000"},
		{name: "leading whitespace", value: " " + valid},
		{name: "trailing whitespace", value: valid + "\n"},
		{name: "short", value: valid[:42]},
		{name: "long", value: valid + "A"},
		{name: "invalid alphabet", value: valid[:42] + "+"},
		{name: "noncanonical trailing bits", value: valid[:42] + "B"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("REQUESTARR_SETUP_TOKEN", tt.value)
			t.Setenv("REQUESTARR_DATABASE_URL", "postgres://requestarr:secret@localhost/requestarr")
			cfg := FromEnv()
			got, ready := cfg.SetupToken()
			if ready != tt.ready {
				t.Fatalf("SetupToken ready = %t, want %t", ready, tt.ready)
			}
			if ready && got != validBytes {
				t.Fatalf("SetupToken decoded unexpected bytes")
			}
			if err := cfg.Validate(); err != nil {
				t.Fatalf("locked token must not invalidate startup: %v", err)
			}
			for _, rendered := range []string{fmt.Sprint(cfg), fmt.Sprintf("%#v", cfg)} {
				if tt.value != "" && strings.Contains(rendered, tt.value) {
					t.Fatalf("configuration rendering exposed setup token: %s", rendered)
				}
				if !strings.Contains(rendered, "SetupToken:<redacted>") {
					t.Fatalf("configuration rendering lacks explicit setup redaction: %s", rendered)
				}
			}
		})
	}
}

func TestSetupTokenIsSnapshotFromEnvironment(t *testing.T) {
	firstBytes := [32]byte{1}
	secondBytes := [32]byte{2}
	t.Setenv("REQUESTARR_SETUP_TOKEN", base64.RawURLEncoding.EncodeToString(firstBytes[:]))
	t.Setenv("REQUESTARR_DATABASE_URL", "postgres://requestarr:secret@localhost/requestarr")
	cfg := FromEnv()
	t.Setenv("REQUESTARR_SETUP_TOKEN", base64.RawURLEncoding.EncodeToString(secondBytes[:]))

	got, ready := cfg.SetupToken()
	if !ready || got != firstBytes {
		t.Fatalf("SetupToken() changed after environment mutation: ready=%t", ready)
	}
}

func TestPublicURLCookiePolicyValidation(t *testing.T) {
	validDatabaseURL := "postgres://requestarr:secret@localhost/requestarr"
	tests := []struct {
		name        string
		environment string
		publicURL   string
		secure      bool
		wantErr     string
	}{
		{name: "production HTTPS", environment: "production", publicURL: "https://requestarr.example", secure: true},
		{name: "development HTTPS", environment: "development", publicURL: "https://requestarr.example:8443", secure: true},
		{name: "development localhost HTTP", environment: "development", publicURL: "http://localhost:8080"},
		{name: "development IPv4 loopback range", environment: "development", publicURL: "http://127.99.1.2:8080"},
		{name: "development IPv6 loopback", environment: "development", publicURL: "http://[::1]:8080"},
		{name: "production loopback HTTP rejected", environment: "production", publicURL: "http://localhost:8080", wantErr: "REQUESTARR_PUBLIC_URL must use https, except for loopback HTTP in development"},
		{name: "non-loopback HTTP rejected", environment: "development", publicURL: "http://192.168.1.10:8080", wantErr: "REQUESTARR_PUBLIC_URL must use https, except for loopback HTTP in development"},
		{name: "localhost suffix rejected", environment: "development", publicURL: "http://localhost.example:8080", wantErr: "REQUESTARR_PUBLIC_URL must use https, except for loopback HTTP in development"},
		{name: "wrong scheme", environment: "development", publicURL: "ftp://localhost:8080", wantErr: "REQUESTARR_PUBLIC_URL must use https, except for loopback HTTP in development"},
		{name: "path rejected", environment: "production", publicURL: "https://requestarr.example/app", wantErr: "REQUESTARR_PUBLIC_URL must be an absolute origin without credentials, path, query, or fragment"},
		{name: "userinfo rejected", environment: "production", publicURL: "https://user:secret@requestarr.example", wantErr: "REQUESTARR_PUBLIC_URL must be an absolute origin without credentials, path, query, or fragment"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{Environment: tt.environment, HTTPAddr: ":8080", PublicURL: tt.publicURL, LogLevelRaw: "info", databaseURL: validDatabaseURL}
			err := cfg.Validate()
			if tt.wantErr == "" && err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
			if tt.wantErr != "" && (err == nil || err.Error() != tt.wantErr) {
				t.Fatalf("Validate() error = %v, want %q", err, tt.wantErr)
			}
			if tt.wantErr == "" && cfg.SetupCookieSecure() != tt.secure {
				t.Fatalf("SetupCookieSecure() = %t, want %t", cfg.SetupCookieSecure(), tt.secure)
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
