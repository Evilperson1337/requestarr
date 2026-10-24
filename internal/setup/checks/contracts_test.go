package checks_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/setup"
	"github.com/evilperson1337/requestarr/internal/setup/checks"
)

func TestDiscoveryMetadataAndEnvironment(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(map[string]string, *checks.Config, *setup.Settings)
		want   setup.CheckCode
	}{
		{"candidate-public", func(_ map[string]string, c *checks.Config, _ *setup.Settings) {
			c.Runtime.OIDCIssuer = "https://old.example/realm"
		}, setup.CheckEnvironment},
		{"client-changed", func(_ map[string]string, c *checks.Config, _ *setup.Settings) { c.Runtime.OIDCClientID = "old-client" }, setup.CheckEnvironment},
		{"secret-missing", func(_ map[string]string, c *checks.Config, _ *setup.Settings) { c.OIDCClientSecret = "" }, setup.CheckEnvironment},
		{"secret-flag-missing", func(_ map[string]string, c *checks.Config, _ *setup.Settings) { c.Runtime.OIDCSecretConfigured = false }, setup.CheckEnvironment},
		{"issuer-mismatch", func(d map[string]string, _ *checks.Config, _ *setup.Settings) {
			d["issuer"] = "https://different.example"
		}, setup.CheckUnavailable},
		{"missing-auth", func(d map[string]string, _ *checks.Config, _ *setup.Settings) { delete(d, "authorization_endpoint") }, setup.CheckUnavailable},
		{"missing-token", func(d map[string]string, _ *checks.Config, _ *setup.Settings) { delete(d, "token_endpoint") }, setup.CheckUnavailable},
		{"missing-jwks", func(d map[string]string, _ *checks.Config, _ *setup.Settings) { delete(d, "jwks_uri") }, setup.CheckUnavailable},
		{"insecure-auth", func(d map[string]string, _ *checks.Config, _ *setup.Settings) {
			d["authorization_endpoint"] = "http://identity.example/auth"
		}, setup.CheckUnavailable},
		{"insecure-token", func(d map[string]string, _ *checks.Config, _ *setup.Settings) {
			d["token_endpoint"] = "http://identity.example/token"
		}, setup.CheckUnavailable},
		{"insecure-jwks", func(d map[string]string, _ *checks.Config, _ *setup.Settings) {
			d["jwks_uri"] = "http://identity.example/keys"
		}, setup.CheckUnavailable},
		{"userinfo", func(d map[string]string, _ *checks.Config, _ *setup.Settings) {
			d["token_endpoint"] = "https://user:secret@identity.example/token"
		}, setup.CheckUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config, settings := readyConfig(), readySettings()
			doc := map[string]string{"issuer": settings.Authentication.Issuer, "authorization_endpoint": "https://identity.example/auth", "token_endpoint": "https://identity.example/token", "jwks_uri": "https://identity.example/keys"}
			tc.change(doc, &config, &settings)
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Header.Get("Authorization") != "" || r.ContentLength > 0 {
					t.Error("discovery sent credentials")
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(doc)
			}))
			defer server.Close()
			config.Client = &http.Client{Transport: routeTo{server.Client().Transport, server.URL}}
			code, err := checks.New(config).Check(context.Background(), setup.TargetAuthentication, settings)
			if code != tc.want || (err != nil) != (tc.want == setup.CheckUnavailable) || calls != 1 {
				t.Fatalf("Check = %q, %v; calls = %d", code, err, calls)
			}
		})
	}
}

func TestIGDBRejectsInvalidTokenBeforePlatforms(t *testing.T) {
	for _, body := range []string{
		`{"access_token":"secret","expires_in":0,"token_type":"bearer"}`,
		`{"access_token":"secret","expires_in":-1,"token_type":"bearer"}`,
		`{"access_token":"secret","expires_in":31536001,"token_type":"bearer"}`,
		`{"access_token":"secret","expires_in":1.5,"token_type":"bearer"}`,
		`{"access_token":"secret","expires_in":3600,"token_type":"other"}`,
		`{"access_token":"","expires_in":3600,"token_type":"bearer"}`,
		`{"access_token":"secret\r\nInjected: secret","expires_in":3600,"token_type":"bearer"}`,
	} {
		t.Run(body, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(body))
			}))
			defer server.Close()
			config := readyConfig()
			config.Client = &http.Client{Transport: routeTo{server.Client().Transport, server.URL}}
			code, err := checks.New(config).Check(context.Background(), setup.TargetIGDB, readySettings())
			if code != setup.CheckUnavailable || err == nil || calls != 1 || strings.Contains(err.Error(), "secret") {
				t.Fatalf("Check = %q, %v; calls = %d", code, err, calls)
			}
		})
	}
}

func TestIGDBPlatformsFailures(t *testing.T) {
	for _, tc := range []struct {
		name, body, contentType string
		status                  int
	}{
		{"empty", `[]`, "application/json", 200}, {"missing-id", `[{}]`, "application/json", 200}, {"zero-id", `[{"id":0}]`, "application/json", 200},
		{"negative-id", `[{"id":-1}]`, "application/json", 200}, {"wrong-shape", `{}`, "application/json", 200},
		{"unauthorized", `{"error":"fixture-access-token"}`, "application/json", 401},
		{"malformed", string(fixture(t, "malformed.json")), "application/json", 200}, {"truncated", string(fixture(t, "truncated.json")), "application/json", 200},
		{"oversized", `[]` + strings.Repeat(" ", 1<<20), "application/json", 200}, {"content-type", `[{"id":6}]`, "text/html", 200},
		{"redirect", `[]`, "application/json", 302},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			token := fixture(t, "oauth.json")
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path == "/oauth2/token" {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write(token)
					return
				}
				w.Header().Set("Content-Type", tc.contentType)
				w.Header().Set("Location", "/leak")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			config := readyConfig()
			config.Client = &http.Client{Transport: routeTo{server.Client().Transport, server.URL}}
			code, err := checks.New(config).Check(context.Background(), setup.TargetIGDB, readySettings())
			if code != setup.CheckUnavailable || err == nil || calls != 2 || strings.Contains(err.Error(), "fixture-access-token") {
				t.Fatalf("Check = %q, %v; calls = %d", code, err, calls)
			}
		})
	}
}

func TestClientIsClonedAndTargetSharesDeadline(t *testing.T) {
	config := readyConfig()
	var firstDeadline time.Time
	calls, redirects := 0, 0
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		deadline, ok := r.Context().Deadline()
		if !ok || time.Until(deadline) > 10*time.Second || time.Until(deadline) <= 0 {
			t.Error("missing bounded target deadline")
		}
		body := `[{"id":6}]`
		if calls == 1 {
			firstDeadline = deadline
			body = string(fixture(t, "oauth.json"))
		} else if !deadline.Equal(firstDeadline) {
			t.Error("OAuth and platforms must share target deadline")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: ioBody(body)}, nil
	})
	client := &http.Client{Transport: transport, Timeout: time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { redirects++; return nil }}
	config.Client = client
	code, err := checks.New(config).Check(context.Background(), setup.TargetIGDB, readySettings())
	if code != setup.CheckOK || err != nil || calls != 2 {
		t.Fatalf("Check = %q, %v; calls = %d", code, err, calls)
	}
	if client.Timeout != time.Minute || client.Transport == nil || client.CheckRedirect == nil {
		t.Fatal("supplied client mutated")
	}
	_ = client.CheckRedirect(nil, nil)
	if redirects != 1 {
		t.Fatal("supplied redirect callback replaced")
	}
}

func ioBody(s string) io.ReadCloser { return io.NopCloser(strings.NewReader(s)) }

func TestRedirectDoesNotMutateSuppliedClientOrSendCookies(t *testing.T) {
	calls, redirects := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Cookie") != "" {
			t.Error("probe sent supplied client's cookies")
		}
		http.Redirect(w, r, "/redirect-target", http.StatusFound)
	}))
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	u, _ := url.Parse(server.URL)
	jar.SetCookies(u, []*http.Cookie{{Name: "session", Value: "not-for-probes"}})
	client := server.Client()
	transport := client.Transport
	client.Jar, client.Timeout = jar, time.Minute
	client.CheckRedirect = func(*http.Request, []*http.Request) error { redirects++; return nil }
	config := checks.Config{Runtime: setup.Runtime{PublicURL: server.URL, Development: true}, Client: client}
	settings := setup.Settings{General: setup.GeneralSettings{InstanceName: "Requestarr", Timezone: "UTC", PublicURL: server.URL}}
	code, err := checks.New(config).Check(context.Background(), setup.TargetGeneral, settings)
	if code != setup.CheckUnavailable || err == nil || calls != 1 || redirects != 0 {
		t.Fatalf("Check = %q, %v; calls=%d redirects=%d", code, err, calls, redirects)
	}
	if client.Transport != transport || client.Jar != jar || client.Timeout != time.Minute {
		t.Fatal("supplied client or transport mutated")
	}
	_ = client.CheckRedirect(nil, nil)
	if redirects != 1 {
		t.Fatal("supplied callback mutated")
	}
}

func TestTruncatedWireResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", strconv.Itoa(100))
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()
	config := checks.Config{Runtime: setup.Runtime{PublicURL: server.URL, Development: true}, Client: server.Client()}
	code, err := checks.New(config).Check(context.Background(), setup.TargetGeneral, setup.Settings{General: setup.GeneralSettings{InstanceName: "Requestarr", Timezone: "UTC", PublicURL: server.URL}})
	if code != setup.CheckUnavailable || err == nil {
		t.Fatalf("Check = %q, %v", code, err)
	}
}

func TestIGDBSecondRequestCancellation(t *testing.T) {
	started := make(chan struct{})
	token := fixture(t, "oauth.json")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth2/token" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(token)
			return
		}
		_, _ = io.Copy(io.Discard, r.Body)
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()
	config := readyConfig()
	config.Client = &http.Client{Transport: routeTo{server.Client().Transport, server.URL}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		select {
		case <-started:
			cancel()
		case <-ctx.Done():
		}
	}()
	code, err := checks.New(config).Check(ctx, setup.TargetIGDB, readySettings())
	<-done
	if code != setup.CheckUnavailable || err == nil || err.Error() != "setup connection check failed" {
		t.Fatalf("Check = %q, %v", code, err)
	}
	select {
	case <-started:
	default:
		t.Fatal("platforms request never started")
	}
}
