package postgres

import "net/url"

// Redact strips credentials from a database URL so it is safe to log. Callers
// must never log a raw configured database URL, since it may carry a password.
// If the value cannot be parsed as a URL, a fixed placeholder is returned
// instead of the original (possibly sensitive) input.
func Redact(databaseURL string) string {
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		return "(unparseable database url)"
	}
	parsed.User = nil
	return parsed.String()
}
