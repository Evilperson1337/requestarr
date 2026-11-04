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

// PrivateHosts is the one rule for which library Source hosts may be reached
// over plain http outside development (D033): the built-in private ranges and
// names plus the operator's additions from REQUESTARR_HTTP_ALLOWED_NETWORKS.
// The zero value knows only the built-ins.
type PrivateHosts struct {
	prefixes []netip.Prefix
	suffixes []string
}

// parsePrivateHosts reads a comma-separated list of CIDRs and hostname
// suffixes. It returns every valid entry plus an error per malformed one.
func parsePrivateHosts(raw string) (PrivateHosts, []error) {
	var out PrivateHosts
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
		if !validHostSuffix(suffix) {
			errs = append(errs, fmt.Errorf("%s: %q is not a valid CIDR or hostname suffix", httpAllowedNetworksEnv, entry))
			continue
		}
		out.suffixes = append(out.suffixes, suffix)
	}
	return out, errs
}

func validHostSuffix(s string) bool {
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

// parsedNetworks is the parse result of REQUESTARR_HTTP_ALLOWED_NETWORKS.
type parsedNetworks struct {
	hosts PrivateHosts
	errs  []error
}

// SetHTTPAllowedNetworks sets the operator's additions to the private-host list
// and parses them once. Malformed entries are skipped here and reported by name
// by ValidateBootstrap and Validate.
func (c *Config) SetHTTPAllowedNetworks(raw string) {
	c.HTTPAllowedNetworks = raw
	c.parsedNetworks = nil
	if strings.TrimSpace(raw) == "" {
		return
	}
	hosts, errs := parsePrivateHosts(raw)
	c.parsedNetworks = &parsedNetworks{hosts: hosts, errs: errs}
}

// PrivateHosts returns the private-host rule in effect for this Config.
func (c Config) PrivateHosts() PrivateHosts {
	if c.parsedNetworks == nil {
		return PrivateHosts{}
	}
	return c.parsedNetworks.hosts
}

func (c Config) httpAllowedNetworksProblems() []string {
	if c.parsedNetworks == nil {
		return nil
	}
	var out []string
	for _, err := range c.parsedNetworks.errs {
		out = append(out, err.Error())
	}
	return out
}

// Contains reports whether a URL host is private by its form alone: a private
// IP literal, a single-label name, or a name under a local suffix.
func (p PrivateHosts) Contains(host string) bool {
	host = strings.ToLower(host)
	if addr, err := netip.ParseAddr(host); err == nil {
		addr = addr.Unmap()
		for _, prefix := range append(append([]netip.Prefix{}, builtinPrivateNets...), p.prefixes...) {
			if prefix.Contains(addr) {
				return true
			}
		}
		return false
	}
	if !strings.Contains(host, ".") {
		return true
	}
	for _, suffix := range append(append([]string{}, builtinPrivateSuffixes...), p.suffixes...) {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return true
		}
	}
	return false
}
