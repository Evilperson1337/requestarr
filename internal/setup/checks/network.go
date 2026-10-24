package checks

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

type trustedDialKey struct{}

func hostname(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
}

func sameOrigin(a, b string) bool {
	left, err := url.Parse(a)
	if err != nil || left.Host == "" {
		return false
	}
	right, err := url.Parse(b)
	return err == nil && strings.EqualFold(left.Scheme, right.Scheme) && strings.EqualFold(left.Host, right.Host)
}

func (c *Checker) allowedIssuer(raw string) bool {
	host := hostname(raw)
	// Exact runtime issuer or the operator-configured public origin is trusted.
	// Trust is not extended to arbitrary private candidates in development mode.
	if raw == c.config.Runtime.OIDCIssuer || sameOrigin(raw, c.config.Runtime.PublicURL) {
		return true
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return false
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		return publicIP(ip)
	}
	// DNS is checked and pinned at the production dial boundary. Injected
	// transports deliberately own resolution so httptest needs no real DNS.
	return true
}

func publicIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.Zone() != "" {
		return false
	}
	// Public IPv6 currently lives in 2000::/3. Reject compatible/translation
	// forms and reserved allocations rather than allowing address aliases.
	if ip.Is6() && !netip.MustParsePrefix("2000::/3").Contains(ip) {
		return false
	}
	for _, prefix := range []string{
		"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4",
		"64:ff9b::/96", "64:ff9b:1::/48", "100::/64", "2001::/23", "2001:db8::/32", "2002::/16",
	} {
		if netip.MustParsePrefix(prefix).Contains(ip) {
			return false
		}
	}
	return true
}

func safeTransport(resolver *net.Resolver) *http.Transport {
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	return &http.Transport{
		// No environment proxy: it could bypass the validated/pinned dial path.
		Proxy: nil,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, errCheck
			}
			trusted, _ := ctx.Value(trustedDialKey{}).(bool)
			ips, err := resolver.LookupNetIP(ctx, "ip", host)
			if err != nil || len(ips) == 0 {
				return nil, errCheck
			}
			// Reject the entire answer if any address is unsafe. Resolve once and
			// dial the numeric result, never re-resolve a validated hostname.
			for _, ip := range ips {
				if !trusted && !publicIP(ip) {
					return nil, errCheck
				}
			}
			for _, ip := range ips {
				conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
				if err == nil {
					return conn, nil
				}
				if ctx.Err() != nil {
					break
				}
			}
			return nil, errCheck
		},
		ForceAttemptHTTP2: true,
		// Do not reuse an operator-trusted connection for an untrusted candidate
		// on the same origin. These are occasional setup probes, not hot paths.
		DisableKeepAlives:      true,
		TLSHandshakeTimeout:    10 * time.Second,
		ResponseHeaderTimeout:  10 * time.Second,
		IdleConnTimeout:        30 * time.Second,
		MaxIdleConns:           10,
		MaxResponseHeaderBytes: 64 << 10,
	}
}
