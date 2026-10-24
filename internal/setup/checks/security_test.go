package checks_test

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/evilperson1337/requestarr/internal/setup"
	"github.com/evilperson1337/requestarr/internal/setup/checks"
)

func TestCandidateIssuerNetworkPolicy(t *testing.T) {
	for _, issuer := range []string{
		"http://127.0.0.1/realm", "http://10.1.2.3/realm", "http://172.16.0.1/realm", "http://192.168.1.1/realm",
		"http://169.254.169.254/realm", "http://100.64.0.1/realm", "http://0.0.0.0/realm", "http://[::1]/realm",
		"http://[fe80::1]/realm", "http://[fd00::1]/realm", "http://[::ffff:127.0.0.1]/realm", "http://localhost/realm",
		"http://[::127.0.0.1]/realm", "http://[4000::1]/realm", "http://[64:ff9b::7f00:1]/realm",
	} {
		t.Run(issuer, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{}`))
			}))
			defer server.Close()
			checker := checks.New(checks.Config{Runtime: setup.Runtime{Development: true}, Client: &http.Client{Transport: routeTo{server.Client().Transport, server.URL}}})
			code, err := checker.Check(context.Background(), setup.TargetAuthentication, setup.Settings{Authentication: setup.AuthenticationSettings{Issuer: issuer, ClientID: "client", ClientSecretEnv: setup.OIDCSecretEnv}})
			if code != setup.CheckUnavailable || err == nil || calls != 0 {
				t.Fatalf("Check = %q, %v; forbidden calls = %d", code, err, calls)
			}
		})
	}
}

func TestPrivateDNSAnswersAreBlockedEvenOnRuntimeIssuerHost(t *testing.T) {
	// A deterministic DNS fixture maps a seemingly public hostname to loopback.
	resolver := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
		client, server := net.Pipe()
		go func() {
			defer server.Close()
			var size [2]byte
			if _, err := io.ReadFull(server, size[:]); err != nil {
				return
			}
			query := make([]byte, int(binary.BigEndian.Uint16(size[:])))
			if _, err := io.ReadFull(server, query); err != nil || len(query) < 16 {
				return
			}
			end := 12
			for end < len(query) && query[end] != 0 {
				end += int(query[end]) + 1
			}
			if end+5 > len(query) {
				return
			}
			end += 5 // zero label, QTYPE, QCLASS; omit any EDNS additional record.
			answer := append([]byte(nil), query[:end]...)
			answer[2], answer[3] = 0x81, 0x80
			answer[6], answer[7] = 0, 1
			answer[10], answer[11] = 0, 0
			kind := binary.BigEndian.Uint16(query[end-4:])
			answer = append(answer, 0xc0, 0x0c, byte(kind>>8), byte(kind), 0, 1, 0, 0, 0, 30)
			if kind == 1 {
				answer = append(answer, 0, 4, 127, 0, 0, 1)
			} else {
				answer = append(answer, 0, 16)
				answer = append(answer, net.ParseIP("::1").To16()...)
			}
			binary.BigEndian.PutUint16(size[:], uint16(len(answer)))
			_, _ = server.Write(append(size[:], answer...))
		}()
		return client, nil
	}}
	for _, trust := range []string{"arbitrary-candidate", "different-path-on-runtime-host", "exact-runtime-issuer", "client-with-default-transport"} {
		t.Run(trust, func(t *testing.T) {
			calls := 0
			var issuer string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]string{"issuer": issuer, "authorization_endpoint": issuer + "/auth", "token_endpoint": issuer + "/token", "jwks_uri": issuer + "/keys"})
			}))
			defer server.Close()
			_, port, _ := net.SplitHostPort(server.Listener.Addr().String())
			issuer = "http://identity.example:" + port + "/untrusted-realm"
			config := checks.Config{Runtime: setup.Runtime{Development: true}, Resolver: resolver}
			if trust == "different-path-on-runtime-host" {
				config.Runtime.OIDCIssuer = "http://identity.example:" + port + "/configured-realm"
			}
			want, wantCalls := setup.CheckUnavailable, 0
			if trust == "exact-runtime-issuer" {
				config.Runtime.OIDCIssuer, config.Runtime.OIDCClientID, config.Runtime.OIDCSecretConfigured = issuer, "client", true
				config.OIDCClientSecret = "not-sent"
				want, wantCalls = setup.CheckOK, 1
			}
			if trust == "client-with-default-transport" {
				config.Client = &http.Client{}
			}
			code, err := checks.New(config).Check(context.Background(), setup.TargetAuthentication, setup.Settings{Authentication: setup.AuthenticationSettings{Issuer: issuer, ClientID: "client", ClientSecretEnv: setup.OIDCSecretEnv}})
			if code != want || (err != nil) != (want == setup.CheckUnavailable) || calls != wantCalls {
				t.Fatalf("Check = %q, %v; private DNS requests = %d", code, err, calls)
			}
		})
	}
}

func TestOperatorTrustedPrivateHosts(t *testing.T) {
	// No injected client: exercise the production transport against an explicitly
	// configured internal issuer, and then an issuer on the configured public host.
	for _, trust := range []string{"issuer", "public-host"} {
		t.Run(trust, func(t *testing.T) {
			var issuer string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]string{"issuer": issuer, "authorization_endpoint": issuer + "/auth", "token_endpoint": issuer + "/token", "jwks_uri": issuer + "/keys"})
			}))
			defer server.Close()
			issuer = server.URL + "/realm"
			runtime := setup.Runtime{Development: true, OIDCClientID: "client", OIDCSecretConfigured: true}
			want := setup.CheckEnvironment
			if trust == "issuer" {
				runtime.OIDCIssuer = issuer
				want = setup.CheckOK
			} else {
				runtime.PublicURL = server.URL
			}
			checker := checks.New(checks.Config{Runtime: runtime, OIDCClientSecret: "not-sent"})
			code, err := checker.Check(context.Background(), setup.TargetAuthentication, setup.Settings{Authentication: setup.AuthenticationSettings{Issuer: issuer, ClientID: "client", ClientSecretEnv: setup.OIDCSecretEnv}})
			if code != want || err != nil {
				t.Fatalf("Check = %q, %v", code, err)
			}
		})
	}
}

func TestConfiguredPublicOriginDoesNotTrustOtherPrivatePorts(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	configured := httptest.NewServer(http.NotFoundHandler())
	defer configured.Close()
	checker := checks.New(checks.Config{Runtime: setup.Runtime{Development: true, PublicURL: configured.URL}})
	code, err := checker.Check(context.Background(), setup.TargetAuthentication, setup.Settings{Authentication: setup.AuthenticationSettings{Issuer: server.URL + "/realm", ClientID: "client", ClientSecretEnv: setup.OIDCSecretEnv}})
	if code != setup.CheckUnavailable || err == nil || calls != 0 {
		t.Fatalf("other private port received a probe: code=%s calls=%d err=%v", code, calls, err)
	}
}
