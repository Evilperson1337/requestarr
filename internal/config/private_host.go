package config

import (
	"fmt"
	"net/netip"
	"strings"
)

const httpAllowedNetworksEnv = "REQUESTARR_HTTP_ALLOWED_NETWORKS"

// builtinPrivateNets are the IP ranges treated as private hosts (D033).
var builtinPrivateNets = []netip.Prefix{
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("100.64.0.0/10"),
}

var builtinPrivateSuffixes = []string{"local", "lan", "internal", "home.arpa"}

// allowedNetworks is the operator's additions to the built-in private-host list.
type allowedNetworks struct {
	prefixes []netip.Prefix
	suffixes []string
}

// parseAllowedNetworks reads a comma-separated list of CIDRs and hostname
// suffixes. It returns every valid entry plus an error per malformed one.
func parseAllowedNetworks(raw string) (allowedNetworks, []error) {
	var out allowedNetworks
	var errs []error
	for _, entry := range strings.Split(raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if strings.Contains(entry, "/") {
			p, err := netip.ParsePrefix(entry)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %q is not a valid CIDR", httpAllowedNetworksEnv, entry))
				continue
			}
			out.prefixes = append(out.prefixes, p.Masked())
			continue
		}
		suffix := strings.ToLower(strings.TrimPrefix(entry, "."))
		if !validSuffix(suffix) {
			errs = append(errs, fmt.Errorf("%s: %q is not a valid CIDR or hostname suffix", httpAllowedNetworksEnv, entry))
			continue
		}
		out.suffixes = append(out.suffixes, suffix)
	}
	return out, errs
}

func validSuffix(s string) bool {
	if s == "" || strings.HasPrefix(s, "-") || strings.Contains(s, "..") || strings.HasSuffix(s, ".") {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '.') {
			return false
		}
	}
	return true
}

func (c Config) httpAllowedNetworksProblems() []string {
	_, errs := parseAllowedNetworks(c.HTTPAllowedNetworks)
	var out []string
	for _, err := range errs {
		out = append(out, err.Error())
	}
	return out
}

// privateHost reports whether a URL host is private by its form alone: a
// private IP literal, a single-label name, or a name under a local suffix.
func privateHost(host string, extra allowedNetworks) bool {
	host = strings.ToLower(host)
	if addr, err := netip.ParseAddr(host); err == nil {
		addr = addr.Unmap()
		for _, p := range builtinPrivateNets {
			if p.Contains(addr) {
				return true
			}
		}
		for _, p := range extra.prefixes {
			if p.Contains(addr) {
				return true
			}
		}
		return false
	}
	if !strings.Contains(host, ".") {
		return true
	}
	for _, s := range append(append([]string{}, builtinPrivateSuffixes...), extra.suffixes...) {
		if host == s || strings.HasSuffix(host, "."+s) {
			return true
		}
	}
	return false
}
