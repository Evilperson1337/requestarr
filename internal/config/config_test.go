package config

import (
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
			config: Config{HTTPAddr: ":8080", LogLevelRaw: "info", databaseURL: validDatabaseURL},
		},
		{
			name:   "hostname and uppercase log level",
			config: Config{HTTPAddr: "localhost:8080", LogLevelRaw: "DEBUG", databaseURL: validDatabaseURL},
		},
		{
			name:   "IPv4 address",
			config: Config{HTTPAddr: "127.0.0.1:8080", LogLevelRaw: "warn", databaseURL: validDatabaseURL},
		},
		{
			name:   "IPv6 address",
			config: Config{HTTPAddr: "[::1]:8080", LogLevelRaw: "warning", databaseURL: validDatabaseURL},
		},
		{
			name:   "highest valid port",
			config: Config{HTTPAddr: ":65535", LogLevelRaw: "error", databaseURL: validDatabaseURL},
		},
		{
			name:    "missing port",
			config:  Config{HTTPAddr: "localhost", LogLevelRaw: "info", databaseURL: validDatabaseURL},
			wantErr: "REQUESTARR_HTTP_ADDR must be in host:port form",
		},
		{
			name:    "non-numeric port",
			config:  Config{HTTPAddr: "localhost:http", LogLevelRaw: "info", databaseURL: validDatabaseURL},
			wantErr: "REQUESTARR_HTTP_ADDR port must be a number between 1 and 65535",
		},
		{
			name:    "zero port",
			config:  Config{HTTPAddr: ":0", LogLevelRaw: "info", databaseURL: validDatabaseURL},
			wantErr: "REQUESTARR_HTTP_ADDR port must be a number between 1 and 65535",
		},
		{
			name:    "port above maximum",
			config:  Config{HTTPAddr: ":65536", LogLevelRaw: "info", databaseURL: validDatabaseURL},
			wantErr: "REQUESTARR_HTTP_ADDR port must be a number between 1 and 65535",
		},
		{
			name:       "invalid log level does not expose value",
			config:     Config{HTTPAddr: ":8080", LogLevelRaw: "secret-log-value", databaseURL: validDatabaseURL},
			wantErr:    "REQUESTARR_LOG_LEVEL must be one of debug, info, warn, warning, or error",
			notInError: "secret-log-value",
		},
		{
			name:    "multiple invalid settings",
			config:  Config{HTTPAddr: "bad-address", LogLevelRaw: "trace", databaseURL: validDatabaseURL},
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
			cfg := Config{HTTPAddr: ":8080", LogLevelRaw: "info", databaseURL: tt.value}
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
