package metadata

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/evilperson1337/requestarr/internal/domain"
)

var (
	ErrEmptyURL   = errors.New("metadata URL is empty")
	ErrURLTooLong = errors.New("metadata URL exceeds 2048 bytes")
)

// ParseURL accepts only absolute HTTP(S) URLs on supported provider hosts.
func ParseURL(raw string) (*url.URL, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, ErrEmptyURL
	}
	if len(raw) > 2048 {
		return nil, ErrURLTooLong
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u == nil || u.Opaque != "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || !strings.EqualFold(u.Scheme, "https") || u.Host == "" {
		return nil, fmt.Errorf("invalid metadata URL")
	}
	host := strings.ToLower(u.Hostname())
	if host == "" || u.Port() != "" && u.Port() != "443" {
		return nil, fmt.Errorf("invalid metadata URL host")
	}
	if _, _, err := domain.ParseAudibleURL(u); err == nil {
		return u, nil
	}
	allowed := host == "themoviedb.org" || host == "www.themoviedb.org" || host == "tmdb.org" || host == "www.tmdb.org" || host == "musicbrainz.org" || host == "www.musicbrainz.org" || host == "igdb.com" || host == "www.igdb.com"
	if !allowed {
		return nil, fmt.Errorf("unsupported metadata URL host")
	}
	return u, nil
}
