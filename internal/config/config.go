package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Config struct {
	Environment string
	HTTPAddr    string
	PublicURL   string
	LogLevelRaw string
	databaseURL string
}

func FromEnv() Config {
	return Config{
		Environment: env("REQUESTARR_ENV", "development"),
		HTTPAddr:    env("REQUESTARR_HTTP_ADDR", ":8080"),
		PublicURL:   env("REQUESTARR_PUBLIC_URL", "http://localhost:8080"),
		LogLevelRaw: env("REQUESTARR_LOG_LEVEL", "info"),
		databaseURL: os.Getenv("REQUESTARR_DATABASE_URL"),
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

	if err := validateDatabaseURL(c.databaseURL); err != nil {
		problems = append(problems, err.Error())
	}

	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

// DatabaseURL returns the validated PostgreSQL connection URL. Callers must
// treat the result as a secret and must not log it or include it in errors.
func (c Config) DatabaseURL() string {
	return c.databaseURL
}

// String deliberately excludes secret-bearing configuration.
func (c Config) String() string {
	return fmt.Sprintf("Config{Environment:%q HTTPAddr:%q PublicURL:%q LogLevelRaw:%q DatabaseURL:<redacted>}", c.Environment, c.HTTPAddr, c.PublicURL, c.LogLevelRaw)
}

// GoString prevents %#v diagnostics from exposing the database URL.
func (c Config) GoString() string {
	return c.String()
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

func validateDatabaseURL(value string) error {
	if value == "" {
		return errors.New("REQUESTARR_DATABASE_URL is required")
	}

	parsed, err := url.Parse(value)
	if err != nil || !parsed.IsAbs() || parsed.Opaque != "" || parsed.Fragment != "" {
		return errors.New("REQUESTARR_DATABASE_URL must be an absolute PostgreSQL URL")
	}
	if parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" {
		return errors.New("REQUESTARR_DATABASE_URL must use postgres:// or postgresql://")
	}
	poolConfig, err := pgxpool.ParseConfig(value)
	if err != nil {
		return errors.New("REQUESTARR_DATABASE_URL is not a valid PostgreSQL URL")
	}
	// pgx permits database and dbname query parameters to override the URL
	// path. Validate the effective target rather than only the parsed path.
	if strings.TrimSpace(poolConfig.ConnConfig.Database) == "" {
		return errors.New("REQUESTARR_DATABASE_URL must select a non-empty database name")
	}

	return nil
}
