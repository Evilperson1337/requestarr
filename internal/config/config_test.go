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

	got := FromEnv()
	want := Config{
		Environment: "development",
		HTTPAddr:    ":8080",
		PublicURL:   "http://localhost:8080",
		LogLevelRaw: "info",
	}

	if got != want {
		t.Fatalf("FromEnv() = %#v, want %#v", got, want)
	}
	if err := got.Validate(); err != nil {
		t.Fatalf("default configuration failed validation: %v", err)
	}
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name       string
		config     Config
		wantErr    string
		notInError string
	}{
		{
			name:   "defaults",
			config: Config{HTTPAddr: ":8080", LogLevelRaw: "info"},
		},
		{
			name:   "hostname and uppercase log level",
			config: Config{HTTPAddr: "localhost:8080", LogLevelRaw: "DEBUG"},
		},
		{
			name:   "IPv4 address",
			config: Config{HTTPAddr: "127.0.0.1:8080", LogLevelRaw: "warn"},
		},
		{
			name:   "IPv6 address",
			config: Config{HTTPAddr: "[::1]:8080", LogLevelRaw: "warning"},
		},
		{
			name:   "highest valid port",
			config: Config{HTTPAddr: ":65535", LogLevelRaw: "error"},
		},
		{
			name:    "missing port",
			config:  Config{HTTPAddr: "localhost", LogLevelRaw: "info"},
			wantErr: "REQUESTARR_HTTP_ADDR must be in host:port form",
		},
		{
			name:    "non-numeric port",
			config:  Config{HTTPAddr: "localhost:http", LogLevelRaw: "info"},
			wantErr: "REQUESTARR_HTTP_ADDR port must be a number between 1 and 65535",
		},
		{
			name:    "zero port",
			config:  Config{HTTPAddr: ":0", LogLevelRaw: "info"},
			wantErr: "REQUESTARR_HTTP_ADDR port must be a number between 1 and 65535",
		},
		{
			name:    "port above maximum",
			config:  Config{HTTPAddr: ":65536", LogLevelRaw: "info"},
			wantErr: "REQUESTARR_HTTP_ADDR port must be a number between 1 and 65535",
		},
		{
			name:       "invalid log level does not expose value",
			config:     Config{HTTPAddr: ":8080", LogLevelRaw: "secret-log-value"},
			wantErr:    "REQUESTARR_LOG_LEVEL must be one of debug, info, warn, warning, or error",
			notInError: "secret-log-value",
		},
		{
			name:    "multiple invalid settings",
			config:  Config{HTTPAddr: "bad-address", LogLevelRaw: "trace"},
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
