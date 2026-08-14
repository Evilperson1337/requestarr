package config

import (
	"errors"
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Environment string
	HTTPAddr    string
	PublicURL   string
	LogLevelRaw string
}

func FromEnv() Config {
	return Config{
		Environment: env("REQUESTARR_ENV", "development"),
		HTTPAddr:    env("REQUESTARR_HTTP_ADDR", ":8080"),
		PublicURL:   env("REQUESTARR_PUBLIC_URL", "http://localhost:8080"),
		LogLevelRaw: env("REQUESTARR_LOG_LEVEL", "info"),
	}
}

func (c Config) Validate() error {
	var problems []string

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
