package domain

import (
	"errors"
	"net/url"
	"strings"
)

// AudibleRegion is an Audible marketplace. It is display data: the ASIN, never
// the region, identifies an audiobook (D031).
type AudibleRegion string

const (
	RegionUS AudibleRegion = "us"
	RegionCA AudibleRegion = "ca"
	RegionUK AudibleRegion = "uk"
	RegionAU AudibleRegion = "au"
	RegionFR AudibleRegion = "fr"
	RegionDE AudibleRegion = "de"
	RegionJP AudibleRegion = "jp"
	RegionIT AudibleRegion = "it"
	RegionIN AudibleRegion = "in"
	RegionES AudibleRegion = "es"
)

// audibleHosts maps an Audible storefront host to its region.
var audibleHosts = map[string]AudibleRegion{
	"audible.com": RegionUS, "audible.ca": RegionCA, "audible.co.uk": RegionUK, "audible.com.au": RegionAU,
	"audible.fr": RegionFR, "audible.de": RegionDE, "audible.co.jp": RegionJP, "audible.it": RegionIT,
	"audible.in": RegionIN, "audible.es": RegionES,
}

// Valid reports whether r is a known marketplace.
func (r AudibleRegion) Valid() bool {
	for _, known := range audibleHosts {
		if r == known {
			return true
		}
	}
	return false
}

var (
	ErrInvalidASIN       = errors.New("invalid ASIN")
	ErrInvalidAudibleURL = errors.New("not an Audible product URL")
)

// ParseASIN trims and upper-cases an ASIN. An ASIN is ten characters: "B" and
// nine letters or digits, or nine digits and a digit or "X" (an ISBN-10).
func ParseASIN(raw string) (string, error) {
	asin := strings.ToUpper(strings.TrimSpace(raw))
	if len(asin) != 10 {
		return "", ErrInvalidASIN
	}
	if asin[0] == 'B' {
		for i := 1; i < 10; i++ {
			if !isASINChar(asin[i]) {
				return "", ErrInvalidASIN
			}
		}
		return asin, nil
	}
	for i := 0; i < 9; i++ {
		if asin[i] < '0' || asin[i] > '9' {
			return "", ErrInvalidASIN
		}
	}
	if last := asin[9]; (last < '0' || last > '9') && last != 'X' {
		return "", ErrInvalidASIN
	}
	return asin, nil
}

func isASINChar(c byte) bool { return c >= '0' && c <= '9' || c >= 'A' && c <= 'Z' }

// ParseAudibleURL extracts the ASIN and marketplace from an Audible product
// URL, https://www.audible.<tld>/pd/[<slug>/]<ASIN>. Amazon URLs are not
// accepted in v1.
func ParseAudibleURL(u *url.URL) (string, AudibleRegion, error) {
	if u == nil || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || (u.Port() != "" && u.Port() != "443" && u.Port() != "80") {
		return "", "", ErrInvalidAudibleURL
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	region, ok := audibleHosts[host]
	if !ok {
		return "", "", ErrInvalidAudibleURL
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 || len(parts) > 3 || parts[0] != "pd" {
		return "", "", ErrInvalidAudibleURL
	}
	asin, err := ParseASIN(parts[len(parts)-1])
	if err != nil {
		return "", "", ErrInvalidAudibleURL
	}
	return asin, region, nil
}
