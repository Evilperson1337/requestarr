package oidc

import (
	"compress/gzip"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeResolver struct {
	addresses []netip.Addr
	err       error
	calls     int
}

func (r *fakeResolver) LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error) {
	r.calls++
	if r.err != nil {
		return nil, r.err
	}
	return append([]netip.Addr(nil), r.addresses...), nil
}

type recordingDialer struct {
	mu        sync.Mutex
	addresses []string
	dial      func(context.Context, string, string) (net.Conn, error)
}

func (d *recordingDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	d.mu.Lock()
	d.addresses = append(d.addresses, address)
	d.mu.Unlock()
	if d.dial != nil {
		return d.dial(ctx, network, address)
	}
	return nil, errors.New("unavailable")
}

func TestResolveRejectsMixedAnswerSetAndAllowsExactCIDR(t *testing.T) {
	resolver := &fakeResolver{addresses: []netip.Addr{netip.MustParseAddr("93.184.216.34"), netip.MustParseAddr("10.1.2.3")}}
	client := NewDiscoveryClient(Options{Resolver: resolver})
	issuer, _ := parseIssuer("https://issuer.example", false)
	_, err := client.resolve(context.Background(), issuer)
	assertFailure(t, err, FailureDestination)

	client = NewDiscoveryClient(Options{Resolver: resolver, AllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}})
	addresses, err := client.resolve(context.Background(), issuer)
	if err != nil {
		t.Fatalf("resolve with exact allowlist failed: %v", err)
	}
	if len(addresses) != 2 || addresses[0].String() != "93.184.216.34" || addresses[1].String() != "10.1.2.3" {
		t.Fatalf("resolve addresses = %v", addresses)
	}
}

func TestResolveLiteralDoesNotUseDNSAndDevelopmentLoopbackPolicy(t *testing.T) {
	resolver := &fakeResolver{err: errors.New("must not be called")}
	client := NewDiscoveryClient(Options{Resolver: resolver, Development: true})
	issuer, _ := parseIssuer("http://127.0.0.1:8080/realms/test", true)
	addresses, err := client.resolve(context.Background(), issuer)
	if err != nil || len(addresses) != 1 || addresses[0].String() != "127.0.0.1" {
		t.Fatalf("literal loopback resolve = %v, %v", addresses, err)
	}
	if resolver.calls != 0 {
		t.Fatalf("literal resolution made %d DNS calls", resolver.calls)
	}
	if _, err := parseIssuer("http://127.0.0.1:8080", false); err == nil {
		t.Fatal("production accepted loopback HTTP")
	}
	if _, err := parseIssuer("http://localhost.example", true); err == nil {
		t.Fatal("development accepted non-loopback HTTP hostname")
	}
}

func TestPublicAddressReviewedSpecialPurposePolicy(t *testing.T) {
	tests := []struct {
		address string
		public  bool
	}{
		{"8.8.8.8", true}, {"0.1.2.3", false}, {"10.0.0.1", false}, {"100.64.0.1", false},
		{"127.0.0.1", false}, {"169.254.1.1", false}, {"192.0.0.1", false}, {"192.0.2.1", false},
		{"192.31.196.1", false}, {"192.52.193.1", false}, {"192.88.99.1", false}, {"192.168.1.1", false},
		{"192.175.48.1", false}, {"198.18.0.1", false}, {"198.51.100.1", false}, {"203.0.113.1", false},
		{"224.0.0.1", false}, {"255.255.255.255", false}, {"::", false}, {"::1", false},
		{"::ffff:10.0.0.1", false}, {"::ffff:8.8.8.8", true}, {"64:ff9b:1::1", false}, {"100::1", false},
		{"2001:db8::1", false}, {"2002::1", false}, {"2620:4f:8000::1", false}, {"3fff::1", false},
		{"5f00::1", false}, {"fc00::1", false}, {"fe80::1", false}, {"ff00::1", false},
		{"2001:1::1", true}, {"2001:3::1", true}, {"2001:20::1", true}, {"2606:4700:4700::1111", true},
		{"fe80::1%en0", false},
	}
	for _, tt := range tests {
		t.Run(tt.address, func(t *testing.T) {
			if got := publicAddress(netip.MustParseAddr(tt.address)); got != tt.public {
				t.Fatalf("publicAddress(%s) = %v, want %v", tt.address, got, tt.public)
			}
		})
	}
}

func TestAllowedAddressCIDROverrideAndDevelopmentHTTPBoundary(t *testing.T) {
	private := netip.MustParseAddr("10.2.3.4")
	client := NewDiscoveryClient(Options{AllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}})
	if !client.allowedAddress(private, "https") || client.allowedAddress(private, "http") {
		t.Fatal("CIDR override did not remain HTTPS-only")
	}
	development := NewDiscoveryClient(Options{Development: true})
	if !development.allowedAddress(netip.MustParseAddr("127.0.0.1"), "http") || development.allowedAddress(private, "http") {
		t.Fatal("development HTTP policy was not loopback-only")
	}
}

func TestPinnedDialerUsesOnlyCheckedAddresses(t *testing.T) {
	dialer := &recordingDialer{}
	client := NewDiscoveryClient(Options{Dialer: dialer, DialTimeout: 20 * time.Millisecond})
	dial := client.pinnedDialer([]netip.Addr{netip.MustParseAddr("192.0.2.10"), netip.MustParseAddr("2001:db8::10")}, "8443")
	_, err := dial(context.Background(), "tcp", "issuer.example:8443")
	if err == nil {
		t.Fatal("pinned dial unexpectedly succeeded")
	}
	want := []string{"192.0.2.10:8443", "[2001:db8::10]:8443"}
	if strings.Join(dialer.addresses, ",") != strings.Join(want, ",") {
		t.Fatalf("dialed %v, want %v", dialer.addresses, want)
	}
	if _, err := dial(context.Background(), "tcp", "issuer.example:443"); err == nil {
		t.Fatal("pinned dial accepted a changed port")
	}
}

func TestPinnedDialerContinuesAfterFailures(t *testing.T) {
	left, right := net.Pipe()
	defer right.Close()
	dialer := &recordingDialer{dial: func(_ context.Context, _, address string) (net.Conn, error) {
		if strings.HasPrefix(address, "203.0.113.3:") {
			return left, nil
		}
		return nil, errors.New("failed")
	}}
	client := NewDiscoveryClient(Options{Dialer: dialer})
	dial := client.pinnedDialer([]netip.Addr{netip.MustParseAddr("192.0.2.1"), netip.MustParseAddr("198.51.100.2"), netip.MustParseAddr("203.0.113.3")}, "443")
	connection, err := dial(context.Background(), "tcp", "issuer.example:443")
	if err != nil || connection != left || len(dialer.addresses) != 3 {
		t.Fatalf("connection=%v calls=%v err=%v", connection, dialer.addresses, err)
	}
	_ = connection.Close()
}

func TestDiscoverOneDerivedGETAndSecretFreeHeaders(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodGet || r.URL.Path != "/realm/.well-known/openid-configuration" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("User-Agent") != userAgent || r.Header.Get("Accept") != "application/json" {
			t.Errorf("headers = %#v", r.Header)
		}
		for _, forbidden := range []string{"Authorization", "Cookie", "X-Requestarr-CSRF"} {
			if r.Header.Get(forbidden) != "" {
				t.Errorf("request contained %s", forbidden)
			}
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = io.WriteString(w, validMetadata(serverURLFromRequest(r)+"/realm", "none"))
	}))
	defer server.Close()

	issuer := server.URL + "/realm"
	client := NewDiscoveryClient(Options{Development: true})
	evidence, err := client.Discover(context.Background(), DiscoveryInput{Issuer: issuer, ClientType: ClientPublic})
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if calls != 1 || evidence.PolicyVersion != PolicyVersion || evidence.Issuer != issuer || len(evidence.MetadataDigest) != 64 {
		t.Fatalf("calls=%d evidence=%+v", calls, evidence)
	}
	if strings.Contains(evidence.String(), issuer) {
		t.Fatalf("Evidence rendering exposed issuer: %s", evidence.String())
	}
}

func TestDiscoverRejectsRedirectWrongMediaTypeAndOversize(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		kind    FailureKind
	}{
		{name: "redirect", kind: FailureUnreachable, handler: func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Location", "/elsewhere")
			w.WriteHeader(http.StatusFound)
		}},
		{name: "media type", kind: FailureMetadata, handler: func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			_, _ = io.WriteString(w, "{}")
		}},
		{name: "empty media type", kind: FailureMetadata, handler: func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, "{}")
		}},
		{name: "malformed media type", kind: FailureMetadata, handler: func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", `application/json; charset="`)
			_, _ = io.WriteString(w, "{}")
		}},
		{name: "oversize", kind: FailureMetadata, handler: func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, strings.Repeat("x", maxDiscoveryResponse+1))
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()
			_, err := NewDiscoveryClient(Options{Development: true}).Discover(context.Background(), DiscoveryInput{Issuer: server.URL, ClientType: ClientPublic})
			assertFailure(t, err, tt.kind)
		})
	}
}

func TestDiscoverRedirectRefusesSecondRequestAndIgnoresProxyEnvironment(t *testing.T) {
	var first, second int
	secondServer := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { second++ }))
	defer secondServer.Close()
	firstServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		first++
		http.Redirect(w, r, secondServer.URL, http.StatusFound)
	}))
	defer firstServer.Close()
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	_, err := NewDiscoveryClient(Options{Development: true}).Discover(context.Background(), DiscoveryInput{Issuer: firstServer.URL, ClientType: ClientPublic})
	assertFailure(t, err, FailureUnreachable)
	if first != 1 || second != 0 {
		t.Fatalf("requests first=%d second=%d", first, second)
	}
}

func TestDiscoverGzipLimitAppliesToDecompressedBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Encoding", "gzip")
		writer := gzip.NewWriter(w)
		_, _ = io.WriteString(writer, strings.Repeat("x", maxDiscoveryResponse+1))
		_ = writer.Close()
	}))
	defer server.Close()
	_, err := NewDiscoveryClient(Options{Development: true}).Discover(context.Background(), DiscoveryInput{Issuer: server.URL, ClientType: ClientPublic})
	assertFailure(t, err, FailureMetadata)
}

func TestDiscoverHonorsCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewDiscoveryClient(Options{Development: true}).Discover(ctx, DiscoveryInput{Issuer: server.URL, ClientType: ClientPublic})
	assertFailure(t, err, FailureUnavailable)
}

func TestDiscoverOverallDeadline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { time.Sleep(200 * time.Millisecond) }))
	defer server.Close()
	started := time.Now()
	_, err := NewDiscoveryClient(Options{Development: true, OverallTimeout: 25 * time.Millisecond}).Discover(context.Background(), DiscoveryInput{Issuer: server.URL, ClientType: ClientPublic})
	assertFailure(t, err, FailureUnavailable)
	if time.Since(started) > 150*time.Millisecond {
		t.Fatal("overall deadline did not bound operation")
	}
}

func TestDiscoverPinnedTLSUsesHostnameSNIAndTLS12(t *testing.T) {
	certificate := testCertificate(t, "issuer.example")
	var sni string
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil || r.TLS.Version < 0x0303 || r.Host == "" {
			t.Errorf("TLS state=%+v host=%q", r.TLS, r.Host)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, validMetadata("https://"+r.Host, "none"))
	}))
	server.TLS = &tls.Config{Certificates: []tls.Certificate{certificate}, GetConfigForClient: func(info *tls.ClientHelloInfo) (*tls.Config, error) {
		sni = info.ServerName
		return nil, nil
	}}
	server.StartTLS()
	defer server.Close()
	_, port, _ := net.SplitHostPort(server.Listener.Addr().String())
	issuer := "https://issuer.example:" + port
	resolver := &fakeResolver{addresses: []netip.Addr{netip.MustParseAddr("127.0.0.1")}}
	pool := x509.NewCertPool()
	leaf, _ := x509.ParseCertificate(certificate.Certificate[0])
	pool.AddCert(leaf)
	client := NewDiscoveryClient(Options{Resolver: resolver, AllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}, RootCAs: pool})
	_, err := client.Discover(context.Background(), DiscoveryInput{Issuer: issuer, ClientType: ClientPublic})
	if err != nil {
		t.Fatalf("Discover TLS: %v", err)
	}
	if sni != "issuer.example" {
		t.Fatalf("SNI=%q", sni)
	}
}

func TestDiscoverPinnedTLSRejectsWrongHostnameAndTLS11(t *testing.T) {
	tests := []struct {
		name        string
		certificate string
		maxVersion  uint16
	}{
		{name: "wrong hostname", certificate: "other.example"},
		{name: "TLS 1.1", certificate: "issuer.example", maxVersion: tls.VersionTLS11},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			certificate := testCertificate(t, tt.certificate)
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
			server.TLS = &tls.Config{Certificates: []tls.Certificate{certificate}, MaxVersion: tt.maxVersion}
			server.StartTLS()
			defer server.Close()
			_, port, _ := net.SplitHostPort(server.Listener.Addr().String())
			pool := x509.NewCertPool()
			leaf, _ := x509.ParseCertificate(certificate.Certificate[0])
			pool.AddCert(leaf)
			client := NewDiscoveryClient(Options{
				Resolver:     &fakeResolver{addresses: []netip.Addr{netip.MustParseAddr("127.0.0.1")}},
				AllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}, RootCAs: pool,
			})
			_, err := client.Discover(context.Background(), DiscoveryInput{Issuer: "https://issuer.example:" + port, ClientType: ClientPublic})
			assertFailure(t, err, FailureUnreachable)
		})
	}
}

func TestParseMetadataStrictCapabilities(t *testing.T) {
	issuer := "https://issuer.example/realm"
	input := DiscoveryInput{Issuer: issuer, ClientType: ClientConfidential}
	valid := validMetadata(issuer, "client_secret_basic")
	if _, err := parseMetadata([]byte(valid), input, false); err != nil {
		t.Fatalf("valid metadata rejected: %v", err)
	}
	tests := []struct {
		name string
		body string
	}{
		{name: "duplicate", body: strings.Replace(valid, `"issuer":`, `"issuer":"other","issuer":`, 1)},
		{name: "trailing", body: valid + `{}`},
		{name: "issuer mismatch", body: strings.Replace(valid, issuer, "https://other.example", 1)},
		{name: "missing code", body: strings.Replace(valid, `["code"]`, `["token"]`, 1)},
		{name: "missing subject", body: strings.Replace(valid, `["public"]`, `["ephemeral"]`, 1)},
		{name: "missing RS256", body: strings.Replace(valid, `["RS256"]`, `["ES256"]`, 1)},
		{name: "missing S256", body: strings.Replace(valid, `["S256"]`, `["plain"]`, 1)},
		{name: "wrong client auth", body: strings.Replace(valid, `["client_secret_basic"]`, `["none"]`, 1)},
		{name: "userinfo endpoint", body: strings.Replace(valid, issuer+"/authorize", "https://user@private.example/authorize", 1)},
		{name: "zone endpoint", body: strings.Replace(valid, issuer+"/authorize", "https://[fe80::1%25en0]/authorize", 1)},
		{name: "missing auth methods", body: strings.Replace(valid, `,"token_endpoint_auth_methods_supported":["client_secret_basic"]`, "", 1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseMetadata([]byte(tt.body), input, false)
			assertFailure(t, err, FailureMetadata)
		})
	}
}

func TestParseMetadataAcceptsStructurallySafeUntrustedEndpointDestinationsWithoutNetwork(t *testing.T) {
	issuer := "https://issuer.example/realm"
	metadata := validMetadata(issuer, "none")
	metadata = strings.Replace(metadata, issuer+"/authorize", "https://10.0.0.8/authorize", 1)
	metadata = strings.Replace(metadata, issuer+"/token", "https://mixed.private.example/token", 1)
	metadata = strings.Replace(metadata, issuer+"/certs", "https://[fd00::8]/certs", 1)
	if _, err := parseMetadata([]byte(metadata), DiscoveryInput{Issuer: issuer, ClientType: ClientPublic}, false); err != nil {
		t.Fatalf("structurally safe endpoints rejected: %v", err)
	}
}

func TestParseMetadataBoundsUTF8EmptyAndMalformed(t *testing.T) {
	issuer := "https://issuer.example"
	valid := validMetadata(issuer, "none")
	tests := [][]byte{
		nil, []byte(""), []byte("{"), []byte{0xff},
		[]byte(strings.Replace(valid, issuer+`","authorization_endpoint`, strings.Repeat("a", maxMetadataString+1)+`","authorization_endpoint`, 1)),
		[]byte(strings.Replace(valid, `["code"]`, `[]`, 1)),
		[]byte(strings.Replace(valid, `["code"]`, `["`+strings.Repeat("a", 257)+`"]`, 1)),
		[]byte(strings.Replace(valid, `["code"]`, `[`+strings.Repeat(`"x",`, maxMetadataArray)+`"x"]`, 1)),
	}
	for index, body := range tests {
		if _, err := parseMetadata(body, DiscoveryInput{Issuer: issuer, ClientType: ClientPublic}, false); err == nil {
			t.Fatalf("case %d accepted", index)
		}
	}
}

func validMetadata(issuer, authMethod string) string {
	return `{"issuer":"` + issuer + `","authorization_endpoint":"` + issuer + `/authorize","token_endpoint":"` + issuer +
		`/token","jwks_uri":"` + issuer + `/certs","response_types_supported":["code"],"subject_types_supported":["public"],` +
		`"id_token_signing_alg_values_supported":["RS256"],"code_challenge_methods_supported":["S256"],` +
		`"token_endpoint_auth_methods_supported":["` + authMethod + `"]}`
}

func serverURLFromRequest(r *http.Request) string {
	return "http://" + r.Host
}

func assertFailure(t *testing.T, err error, kind FailureKind) {
	t.Helper()
	var failure *Failure
	if !errors.As(err, &failure) || failure.Kind != kind {
		t.Fatalf("error = %#v, want failure kind %v", err, kind)
	}
}
