package config

import (
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Config struct {
	Environment          string
	HTTPAddr             string
	PublicURL            string
	LogLevelRaw          string
	databaseURL          string
	setupToken           [32]byte
	setupReady           bool
	encryptionKey        [32]byte
	encryptionKeyStatus  EncryptionKeyStatus
	secretReuseInvalid   bool
	oidcDestinationCIDRs []netip.Prefix
	oidcCIDRsInvalid     bool
	absDestinationCIDRs  []netip.Prefix
	absCIDRsInvalid      bool
}

// EncryptionKeyStatus describes key availability without exposing key material.
type EncryptionKeyStatus uint8

const (
	EncryptionKeyMissing EncryptionKeyStatus = iota
	EncryptionKeyInvalid
	EncryptionKeyReady
)

func FromEnv() Config {
	cfg := Config{
		Environment: env("REQUESTARR_ENV", "development"),
		HTTPAddr:    env("REQUESTARR_HTTP_ADDR", ":8080"),
		PublicURL:   env("REQUESTARR_PUBLIC_URL", "http://localhost:8080"),
		LogLevelRaw: env("REQUESTARR_LOG_LEVEL", "info"),
		databaseURL: os.Getenv("REQUESTARR_DATABASE_URL"),
	}
	cfg.setupToken, cfg.setupReady = parseSetupToken(os.Getenv("REQUESTARR_SETUP_TOKEN"))
	cfg.encryptionKey, cfg.encryptionKeyStatus = parseEncryptionKey(os.Getenv("REQUESTARR_ENCRYPTION_KEY"))
	cfg.oidcDestinationCIDRs, cfg.oidcCIDRsInvalid = parseExactCIDRs(os.Getenv("REQUESTARR_OIDC_DESTINATION_CIDRS"))
	cfg.absDestinationCIDRs, cfg.absCIDRsInvalid = parseExactCIDRs(os.Getenv("REQUESTARR_ABS_DESTINATION_CIDRS"))
	if cfg.setupReady && cfg.encryptionKeyStatus == EncryptionKeyReady && subtle.ConstantTimeCompare(cfg.setupToken[:], cfg.encryptionKey[:]) == 1 {
		cfg.encryptionKeyStatus = EncryptionKeyInvalid
		cfg.secretReuseInvalid = true
	}
	return cfg
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
	if err := validatePublicURL(c.Environment, c.PublicURL); err != nil {
		problems = append(problems, err.Error())
	}
	if c.secretReuseInvalid {
		problems = append(problems, "deployment secret configuration is invalid")
	}
	if c.oidcCIDRsInvalid {
		problems = append(problems, "REQUESTARR_OIDC_DESTINATION_CIDRS must be a comma-separated list of unique canonical CIDR prefixes")
	}
	if c.absCIDRsInvalid {
		problems = append(problems, "REQUESTARR_ABS_DESTINATION_CIDRS must be a comma-separated list of unique canonical CIDR prefixes")
	}

	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

// ValidateMaintenanceDatabase validates only the setting required by database
// maintenance commands. Maintenance must not be blocked by unrelated runtime
// settings, and callers must not include the connection URL in diagnostics.
func (c Config) ValidateMaintenanceDatabase() error {
	return validateDatabaseURL(c.databaseURL)
}

// DatabaseURL returns the validated PostgreSQL connection URL. Callers must
// treat the result as a secret and must not log it or include it in errors.
func (c Config) DatabaseURL() string {
	return c.databaseURL
}

// SetupToken returns decoded bootstrap key material only to the application
// composition boundary. A false result is the intentional fail-closed locked
// state for either an absent or malformed deployment token.
func (c Config) SetupToken() ([32]byte, bool) {
	return c.setupToken, c.setupReady
}

// EncryptionKey returns a fixed-size snapshot and its non-secret state. The
// returned key is a copy and must never be logged or formatted.
func (c Config) EncryptionKey() ([32]byte, EncryptionKeyStatus) {
	return c.encryptionKey, c.encryptionKeyStatus
}

// OIDCDestinationCIDRs returns a defensive copy of exact destination prefixes.
// The configured topology is intentionally omitted from formatted Config values.
func (c Config) OIDCDestinationCIDRs() []netip.Prefix {
	return append([]netip.Prefix(nil), c.oidcDestinationCIDRs...)
}

// ABSDestinationCIDRs returns a defensive copy of exact destinations allowed
// for a future self-hosted Audiobookshelf source. It is deliberately separate
// from OIDC topology and is omitted from formatted Config values.
func (c Config) ABSDestinationCIDRs() []netip.Prefix {
	return append([]netip.Prefix(nil), c.absDestinationCIDRs...)
}

// SetupCookieSecure reports the validated environment-selected cookie policy.
func (c Config) SetupCookieSecure() bool {
	parsed, err := url.Parse(c.PublicURL)
	return err == nil && parsed.Scheme == "https"
}

// String deliberately excludes secret-bearing configuration.
func (c Config) String() string {
	return fmt.Sprintf("Config{Environment:%q HTTPAddr:%q PublicURL:%q LogLevelRaw:%q DatabaseURL:<redacted> SetupToken:<redacted> EncryptionKey:<redacted> OIDCDestinationCIDRs:<redacted:%d> ABSDestinationCIDRs:<redacted:%d>}", c.Environment, c.HTTPAddr, c.PublicURL, c.LogLevelRaw, len(c.oidcDestinationCIDRs), len(c.absDestinationCIDRs))
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

func parseSetupToken(value string) ([32]byte, bool) {
	var token [32]byte
	if len(value) != base64.RawURLEncoding.EncodedLen(len(token)) {
		return token, false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(decoded) != len(token) || base64.RawURLEncoding.EncodeToString(decoded) != value {
		return token, false
	}
	copy(token[:], decoded)
	return token, true
}

func parseEncryptionKey(value string) ([32]byte, EncryptionKeyStatus) {
	var key [32]byte
	if value == "" {
		return key, EncryptionKeyMissing
	}
	if len(value) != base64.RawURLEncoding.EncodedLen(len(key)) {
		return key, EncryptionKeyInvalid
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(decoded) != len(key) || base64.RawURLEncoding.EncodeToString(decoded) != value {
		return key, EncryptionKeyInvalid
	}
	copy(key[:], decoded)
	return key, EncryptionKeyReady
}

func parseExactCIDRs(value string) ([]netip.Prefix, bool) {
	if value == "" {
		return nil, false
	}
	parts := strings.Split(value, ",")
	prefixes := make([]netip.Prefix, 0, len(parts))
	seen := make(map[netip.Prefix]struct{}, len(parts))
	for _, part := range parts {
		if part == "" || strings.TrimSpace(part) != part || strings.Contains(part, "%") {
			return nil, true
		}
		prefix, err := netip.ParsePrefix(part)
		if err != nil || !prefix.IsValid() || prefix != prefix.Masked() || prefix.String() != part {
			return nil, true
		}
		if _, exists := seen[prefix]; exists {
			return nil, true
		}
		seen[prefix] = struct{}{}
		prefixes = append(prefixes, prefix)
	}
	return prefixes, false
}

func validatePublicURL(environment, value string) error {
	parsed, err := url.Parse(value)
	if err != nil || !parsed.IsAbs() || parsed.Opaque != "" || parsed.User != nil || parsed.Host == "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("REQUESTARR_PUBLIC_URL must be an absolute origin without credentials, path, query, or fragment")
	}
	if parsed.Scheme == "https" {
		return nil
	}
	if parsed.Scheme != "http" {
		return errors.New("REQUESTARR_PUBLIC_URL must use https, except for loopback HTTP in development")
	}
	if !strings.EqualFold(environment, "development") || !loopbackHost(parsed.Hostname()) {
		return errors.New("REQUESTARR_PUBLIC_URL must use https, except for loopback HTTP in development")
	}
	return nil
}

func loopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
