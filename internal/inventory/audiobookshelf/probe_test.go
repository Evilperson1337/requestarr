package audiobookshelf

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"git.example.com/evilperson1337/Requestarr/internal/inventory"
)

func TestProbeEnumeratesLibrariesExactlyOnce(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/candidate-path/api/libraries" || r.URL.RawQuery != "" || r.Method != http.MethodGet {
			t.Fatalf("request = %s %s", r.Method, r.URL.String())
		}
		if got := r.Header.Get("Authorization"); got != "Bearer probe-token" {
			t.Fatalf("authorization = %q", got)
		}
		if got := r.Header.Get("User-Agent"); got != userAgent {
			t.Fatalf("user agent = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[{"id":"fiction","name":"Fiction"}]`)
	}))
	defer server.Close()
	result, err := testProbe(t, server.URL+"/candidate-path").Probe(context.Background(), inventory.ConnectionProbeRequest{EndpointURL: "http://abs.example/candidate-path", Credential: "probe-token"})
	if err != nil {
		t.Fatalf("Probe() error = %v", err)
	}
	if calls.Load() != 1 || len(result.Libraries) != 1 || result.Libraries[0] != (inventory.RemoteLibrary{ID: "fiction", Name: "Fiction"}) {
		t.Fatalf("calls=%d result=%+v", calls.Load(), result)
	}
}

func TestProbeCategorizesSafeFailuresWithoutCredentialLeakage(t *testing.T) {
	const credential = "probe-secret-token"
	oversized := strings.Repeat("x", maxResponseBytes+1)
	tests := []struct {
		name     string
		status   int
		content  string
		body     string
		expected error
	}{
		{name: "unauthorized", status: http.StatusUnauthorized, expected: inventory.ErrProbeAuthenticationFailed},
		{name: "forbidden", status: http.StatusForbidden, expected: inventory.ErrProbeAuthenticationFailed},
		{name: "status", status: http.StatusBadGateway, content: "application/json", body: `[]`, expected: inventory.ErrProbeResponseInvalid},
		{name: "content type", status: http.StatusOK, content: "text/plain", body: `[]`, expected: inventory.ErrProbeResponseInvalid},
		{name: "malformed JSON", status: http.StatusOK, content: "application/json", body: `{`, expected: inventory.ErrProbeResponseInvalid},
		{name: "duplicate JSON key", status: http.StatusOK, content: "application/json", body: `[{"id":"one","id":"two","name":"One"}]`, expected: inventory.ErrProbeResponseInvalid},
		{name: "oversized response", status: http.StatusOK, content: "application/json", body: oversized, expected: inventory.ErrProbeResponseInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tt.content)
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, tt.body)
			}))
			defer server.Close()
			_, err := testProbe(t, server.URL).Probe(context.Background(), inventory.ConnectionProbeRequest{EndpointURL: "http://abs.example/private-path", Credential: credential})
			if !errors.Is(err, tt.expected) {
				t.Fatalf("Probe() error = %v, want %v", err, tt.expected)
			}
			for _, secret := range []string{credential, "private-path", oversized} {
				if strings.Contains(err.Error(), secret) {
					t.Fatalf("Probe() leaked %q in %v", secret, err)
				}
			}
		})
	}
}

func TestProbeRejectsRedirectAndDestinationFailures(t *testing.T) {
	redirected := false
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { redirected = true }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer server.Close()
	_, err := testProbe(t, server.URL).Probe(context.Background(), inventory.ConnectionProbeRequest{EndpointURL: "http://abs.example", Credential: "token"})
	if !errors.Is(err, inventory.ErrProbeResponseInvalid) || redirected {
		t.Fatalf("redirect error=%v followed=%v", err, redirected)
	}

	probe := NewProbe(ProbeOptions{Resolver: fixedResolver{netip.MustParseAddr("127.0.0.1")}, AllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")}})
	_, err = probe.Probe(context.Background(), inventory.ConnectionProbeRequest{EndpointURL: "http://abs.example", Credential: "token"})
	if !errors.Is(err, inventory.ErrProbeInvalidDestination) {
		t.Fatalf("denied DNS error = %v", err)
	}
	_, err = probe.Probe(context.Background(), inventory.ConnectionProbeRequest{EndpointURL: "ftp://abs.example", Credential: "token"})
	if !errors.Is(err, inventory.ErrProbeInvalidDestination) {
		t.Fatalf("bad destination error = %v", err)
	}
}

func TestProbeHandlesStalledBodyAndUnavailableTester(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, "[")
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		<-time.After(time.Second)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := testProbe(t, server.URL).Probe(ctx, inventory.ConnectionProbeRequest{EndpointURL: "http://abs.example", Credential: "token"})
	if !errors.Is(err, inventory.ErrProbeResponseInvalid) {
		t.Fatalf("stalled body error = %v", err)
	}
	var nilProbe *Probe
	_, err = nilProbe.Probe(context.Background(), inventory.ConnectionProbeRequest{EndpointURL: "http://abs.example", Credential: "token"})
	if !errors.Is(err, inventory.ErrProbeTesterUnavailable) {
		t.Fatalf("nil probe error = %v", err)
	}
}

func testProbe(t *testing.T, rawURL string) *Probe {
	t.Helper()
	base, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	return NewProbe(ProbeOptions{
		Resolver:     fixedResolver{netip.MustParseAddr("127.0.0.1")},
		Dialer:       forwardingDialer{target: base.Host},
		AllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")},
	})
}
