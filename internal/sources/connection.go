package sources

import (
	"errors"
	"net/url"
	"strings"
	"unicode/utf8"
)

// Connection is where one Source lives: the part an operator saves in Settings
// (D028). Credentials are stored secrets, never part of a Connection.
type Connection struct {
	Slug     string
	URL      string
	Username string
}

// ErrInvalidConnection is returned for a URL or username that cannot be used.
var ErrInvalidConnection = errors.New("invalid library connection")

// Validate checks a Connection against its kind. The URL must be an http(s)
// origin with no credentials, query or fragment; only kinds with a username
// credential accept one.
func (c Connection) Validate() error {
	kind, ok := Lookup(c.Slug)
	if !ok {
		return ErrInvalidConnection
	}
	u, err := url.Parse(c.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || len(c.URL) > 2048 {
		return ErrInvalidConnection
	}
	if _, takesUsername := kind.Credential(RoleUsername); !takesUsername {
		if c.Username != "" {
			return ErrInvalidConnection
		}
		return nil
	}
	if c.Username == "" || len(c.Username) > 256 || !utf8.ValidString(c.Username) || strings.TrimSpace(c.Username) != c.Username || strings.ContainsAny(c.Username, "\x00\r\n") {
		return ErrInvalidConnection
	}
	return nil
}
