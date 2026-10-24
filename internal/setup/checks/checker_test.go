package checks_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/evilperson1337/requestarr/internal/setup"
	"github.com/evilperson1337/requestarr/internal/setup/checks"
)

func TestGeneralConfiguredHealth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/healthz" || r.UserAgent() == "" {
			t.Errorf("unexpected health request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()
	checker := checks.New(checks.Config{Runtime: setup.Runtime{PublicURL: server.URL, Development: true}, Client: server.Client()})
	var seam setup.Checker = checker
	settings := setup.Settings{General: setup.GeneralSettings{InstanceName: "Requestarr", Timezone: "UTC", PublicURL: server.URL}}
	code, err := seam.Check(context.Background(), setup.TargetGeneral, settings)
	if err != nil || code != setup.CheckOK {
		t.Fatalf("Check = %q, %v", code, err)
	}
}

func TestIGDBOAuthAndPlatforms(t *testing.T) {
	token, platforms := fixture(t, "oauth.json"), fixture(t, "platforms.json")
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		if calls == 1 {
			if r.Host != "id.twitch.tv" || r.Method != http.MethodPost || r.URL.RequestURI() != "/oauth2/token" || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
				t.Error("unexpected OAuth endpoint")
			}
			if err := r.ParseForm(); err != nil {
				t.Error(err)
			}
			if r.PostForm.Get("client_id") != "igdb-client" || r.PostForm.Get("client_secret") != "igdb-secret" || r.PostForm.Get("grant_type") != "client_credentials" || r.URL.RawQuery != "" {
				t.Error("unexpected OAuth form")
			}
			_, _ = w.Write(token)
			return
		}
		body, _ := io.ReadAll(r.Body)
		if r.Host != "api.igdb.com" || r.Method != http.MethodPost || r.URL.RequestURI() != "/v4/platforms" || string(body) != "fields id; limit 1;" || r.Header.Get("Authorization") != "Bearer fixture-access-token" || r.Header.Get("Client-ID") != "igdb-client" {
			t.Error("unexpected IGDB platform request")
		}
		_, _ = w.Write(platforms)
	}))
	defer server.Close()
	checker := checks.New(checks.Config{Runtime: setup.Runtime{IGDBClientID: "igdb-client", IGDBSecretConfigured: true}, IGDBClientSecret: "igdb-secret", Client: &http.Client{Transport: routeTo{server.Client().Transport, server.URL}}})
	code, err := checker.Check(context.Background(), setup.TargetIGDB, setup.Settings{Providers: setup.ProviderSettings{IGDBEnabled: true, IGDBClientID: "igdb-client", IGDBClientSecretEnv: setup.IGDBSecretEnv}})
	if err != nil || code != setup.CheckOK || calls != 2 {
		t.Fatalf("Check = %q, %v; calls %d", code, err, calls)
	}
}

func TestMusicBrainzArtist(t *testing.T) {
	data := fixture(t, "musicbrainz.json")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "musicbrainz.org" || r.Method != http.MethodGet || r.URL.RequestURI() != "/ws/2/artist/5b11f4ce-a62d-471e-81fc-a69a8278c7da?fmt=json" || !strings.Contains(r.UserAgent(), "Requestarr") || !strings.Contains(r.UserAgent(), "admin@example.org") {
			t.Error("unexpected MusicBrainz request")
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write(data)
	}))
	defer server.Close()
	checker := checks.New(checks.Config{Client: &http.Client{Transport: routeTo{server.Client().Transport, server.URL}}})
	code, err := checker.Check(context.Background(), setup.TargetMusicBrainz, setup.Settings{Providers: setup.ProviderSettings{MusicBrainzEnabled: true, MusicBrainzContact: "admin@example.org"}})
	if err != nil || code != setup.CheckOK {
		t.Fatalf("Check = %q, %v", code, err)
	}
}

// routeTo preserves the fixed production URL at the test boundary and routes
// only the wire connection to the deterministic server.
type routeTo struct {
	base      http.RoundTripper
	serverURL string
}

func (r routeTo) RoundTrip(req *http.Request) (*http.Response, error) {
	copy := req.Clone(req.Context())
	u, _ := http.NewRequest(http.MethodGet, r.serverURL, nil)
	copy.URL.Scheme, copy.URL.Host = u.URL.Scheme, u.URL.Host
	return r.base.RoundTrip(copy)
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestTMDBConfiguration(t *testing.T) {
	data := fixture(t, "tmdb.json")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "api.themoviedb.org" || r.URL.RequestURI() != "/3/configuration" || r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer tmdb-secret" {
			t.Error("unexpected TMDB request")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
	}))
	defer server.Close()
	client := &http.Client{Transport: routeTo{server.Client().Transport, server.URL}}
	checker := checks.New(checks.Config{Runtime: setup.Runtime{TMDBTokenConfigured: true}, TMDBReadToken: "tmdb-secret", Client: client})
	settings := setup.Settings{Providers: setup.ProviderSettings{TMDBEnabled: true, TMDBTokenEnv: setup.TMDBTokenEnv}}
	code, err := checker.Check(context.Background(), setup.TargetTMDB, settings)
	if err != nil || code != setup.CheckOK {
		t.Fatalf("Check = %q, %v", code, err)
	}
}

func TestAuthenticationDiscovery(t *testing.T) {
	var issuer string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/realm/.well-known/openid-configuration" || r.Method != http.MethodGet || r.Header.Get("Authorization") != "" || r.ContentLength > 0 {
			t.Errorf("discovery must be a credential-free GET: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"issuer": issuer, "authorization_endpoint": issuer + "/auth", "token_endpoint": issuer + "/token", "jwks_uri": issuer + "/certs"})
	}))
	defer server.Close()
	issuer = server.URL + "/realm"
	runtime := setup.Runtime{OIDCIssuer: issuer, OIDCClientID: "requestarr", OIDCSecretConfigured: true, Development: true}
	checker := checks.New(checks.Config{Runtime: runtime, OIDCClientSecret: "never-send-this", Client: server.Client()})
	settings := setup.Settings{Authentication: setup.AuthenticationSettings{Issuer: issuer, ClientID: "requestarr", ClientSecretEnv: setup.OIDCSecretEnv}}
	code, err := checker.Check(context.Background(), setup.TargetAuthentication, settings)
	if err != nil || code != setup.CheckOK {
		t.Fatalf("Check = %q, %v", code, err)
	}
}
