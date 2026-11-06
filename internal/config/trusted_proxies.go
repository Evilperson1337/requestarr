package config

import (
	"fmt"
	"net/netip"
	"strings"
)

const trustedProxiesEnv = "REQUESTARR_TRUSTED_PROXIES"

// ParseTrustedProxies parses comma-separated CIDRs and single addresses. The
// error names the variable and the position of the bad entry, never its value.
func ParseTrustedProxies(raw string) ([]netip.Prefix, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var out []netip.Prefix
	for i, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		p, err := netip.ParsePrefix(part)
		if err != nil {
			a, aerr := netip.ParseAddr(part)
			if aerr != nil {
				return nil, fmt.Errorf("%s entry %d is not a valid CIDR or address", trustedProxiesEnv, i+1)
			}
			a = a.Unmap()
			p = netip.PrefixFrom(a, a.BitLen())
		}
		out = append(out, p.Masked())
	}
	return out, nil
}

// TrustedProxyPrefixes is the validated list; invalid input yields none.
func (c Config) TrustedProxyPrefixes() []netip.Prefix {
	p, err := ParseTrustedProxies(c.TrustedProxies)
	if err != nil {
		return nil
	}
	return p
}
