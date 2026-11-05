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

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/secrets"
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
	settings := setup.Settings{General: setup.GeneralSettings{InstanceName: "Requestarr", Timezone: "UTC", PublicURL: server.URL}}
	code, err := checker.Check(context.Background(), setup.TargetGeneral, settings)
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
	checker := checks.New(checks.Config{Runtime: setup.Runtime{IGDBClientID: "igdb-client"}, Secrets: secrets.Static{secrets.IGDBClientSecret: "igdb-secret"}, Client: &http.Client{Transport: routeTo{server.Client().Transport, server.URL}}})
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
	checker := checks.New(checks.Config{Secrets: secrets.Static{secrets.TMDBReadToken: "tmdb-secret"}, Client: client})
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

func TestLibraryChecksUseConfiguredCredentialsAndBoundedEndpointPerSource(t *testing.T) {
	for _, tc := range []struct {
		kind, path, body, wantCredential string
	}{
		{"jellyfin", "/System/Info", `{"ServerName":"test"}`, "X-Emby-Token"},
		{"plex", "/library/sections", `<MediaContainer size="1"/>`, "X-Plex-Token"},
		{"romm", "/api/platforms", `[]`, "Authorization"},
		{"navidrome", "/rest/ping.view", `{"subsonic-response":{"status":"ok"}}`, "u"},
		{"audiobookshelf", "/api/libraries", `{"libraries":[{"id":"l","mediaType":"book"}]}`, "Authorization"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != tc.path {
					t.Errorf("path=%s want %s", r.URL.Path, tc.path)
				}
				switch tc.wantCredential {
				case "X-Emby-Token":
					if r.Header.Get(tc.wantCredential) != "jf-token" {
						t.Error("Jellyfin token absent")
					}
				case "X-Plex-Token":
					if r.Header.Get(tc.wantCredential) != "plex-token" || r.URL.RawQuery != "" {
						t.Error("Plex token absent")
					}
				case "Authorization":
					if got := r.Header.Get(tc.wantCredential); got != "Bearer romm-token" && got != "Bearer abs-key" {
						t.Error("Bearer token absent")
					}
				case "u":
					if r.URL.Query().Get("u") != "music-user" || r.URL.Query().Get("p") != "" || len(r.URL.Query().Get("t")) != 32 || len(r.URL.Query().Get("s")) != 32 {
						t.Error("Navidrome credentials absent")
					}
				}
				w.Header().Set("Content-Type", "application/json")
				if tc.kind == "plex" {
					// Real Plex answers in JSON whenever the client says it accepts JSON,
					// and in XML otherwise (observed against a live server).
					if strings.Contains(r.Header.Get("Accept"), "json") {
						w.Header().Set("Content-Type", "application/json")
						_, _ = io.WriteString(w, `{"MediaContainer":{"size":1}}`)
						return
					}
					w.Header().Set("Content-Type", "text/xml;charset=utf-8")
				}
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			libraries := setup.LibrariesSettings{}
			source := setup.SourceSettings{Enabled: true, URL: "https://library.example", MediaTypes: []domain.MediaType{domain.MediaMovie}}
			cfg := checks.Config{Runtime: setup.Runtime{Development: true}, Client: &http.Client{Transport: routeTo{server.Client().Transport, server.URL}}, Secrets: secrets.Static{secrets.JellyfinAPIKey: "jf-token", secrets.PlexToken: "plex-token", secrets.RommToken: "romm-token", secrets.NavidromePassword: "music-pass", secrets.AudiobookshelfAPIKey: "abs-key"}}
			target := setup.TargetJellyfin
			switch tc.kind {
			case "jellyfin":
				source.TokenEnv = setup.JellyfinAPIKeyEnv
				libraries.Jellyfin = source
			case "plex":
				source.TokenEnv = setup.PlexTokenEnv
				libraries.Plex = source
				target = setup.TargetPlex
			case "romm":
				source.TokenEnv = setup.RommTokenEnv
				libraries.Romm = source
				target = setup.TargetRomm
			case "audiobookshelf":
				source.MediaTypes = []domain.MediaType{domain.MediaAudiobook}
				source.TokenEnv = setup.AudiobookshelfKeyEnv
				libraries.Audiobookshelf = source
				target = setup.TargetAudiobookshelf
			case "navidrome":
				source.MediaTypes = []domain.MediaType{domain.MediaAlbum}
				source.Username = "music-user"
				source.PasswordEnv = setup.NavidromePasswordEnv
				libraries.Navidrome = source
				target = setup.TargetNavidrome
			}
			code, err := checks.New(cfg).Check(context.Background(), target, setup.Settings{Libraries: libraries})
			if err != nil || code != setup.CheckOK {
				t.Fatalf("Check=%s err=%v", code, err)
			}
		})
	}
}

func TestAppriseCheckProbesOnlyTheConfiguredServiceAndNeverSendsTheKey(t *testing.T) {
	var path string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.RequestURI()
		if r.Method != http.MethodGet || strings.Contains(r.URL.String(), "secret-key") || r.Header.Get("Authorization") != "" {
			t.Errorf("unsafe Apprise probe: %s %s", r.Method, r.URL)
		}
		_, _ = w.Write([]byte("OK"))
	}))
	defer server.Close()
	runtime := setup.Runtime{AppriseURL: server.URL, Development: true}
	checker := checks.New(checks.Config{Runtime: runtime, Secrets: secrets.Static{secrets.AppriseConfigKey: "secret-key"}, Client: server.Client()})
	notify := func(n setup.NotificationSettings) setup.CheckCode {
		code, err := checker.Check(context.Background(), setup.TargetNotifications, setup.Settings{Notifications: n})
		if code == setup.CheckOK && err != nil {
			t.Fatal(err)
		}
		return code
	}
	enabled := setup.NotificationSettings{AppriseEnabled: true, AppriseURL: server.URL, AppriseConfigKeyEnv: setup.AppriseConfigKeyEnv}
	if code := notify(enabled); code != setup.CheckOK || path != "/status" {
		t.Fatalf("configured Apprise = %q path %q", code, path)
	}
	if code := notify(setup.NotificationSettings{AppriseConfigKeyEnv: setup.AppriseConfigKeyEnv}); code != setup.CheckDisabled {
		t.Fatalf("disabled = %q", code)
	}
	other := enabled
	other.AppriseURL = "http://169.254.169.254"
	if code := notify(other); code != setup.CheckEnvironment {
		t.Fatalf("unconfigured URL probed: %q", code)
	}
	missing := enabled
	missing.AppriseURL = ""
	if code := notify(missing); code != setup.CheckIncomplete {
		t.Fatalf("incomplete = %q", code)
	}
}

// A key that can see only podcast libraries (or none) is not a working
// audiobook connection, even though Audiobookshelf answers 200.
func TestAudiobookshelfCheckNeedsABookLibrary(t *testing.T) {
	for body, want := range map[string]setup.CheckCode{
		`{"libraries":[{"id":"l","mediaType":"book"},{"id":"p","mediaType":"podcast"}]}`: setup.CheckOK,
		`{"libraries":[{"id":"p","mediaType":"podcast"}]}`:                               setup.CheckUnavailable,
		`{"libraries":[]}`:   setup.CheckUnavailable,
		`<html>login</html>`: setup.CheckUnavailable,
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, body)
		}))
		libraries := setup.LibrariesSettings{Audiobookshelf: setup.SourceSettings{Enabled: true, URL: "https://abs.example", MediaTypes: []domain.MediaType{domain.MediaAudiobook}, TokenEnv: setup.AudiobookshelfKeyEnv}}
		cfg := checks.Config{Runtime: setup.Runtime{Development: true}, Client: &http.Client{Transport: routeTo{server.Client().Transport, server.URL}}, Secrets: secrets.Static{secrets.AudiobookshelfAPIKey: "abs-key"}}
		code, _ := checks.New(cfg).Check(context.Background(), setup.TargetAudiobookshelf, setup.Settings{Libraries: libraries})
		server.Close()
		if code != want {
			t.Errorf("%s: code %q, want %q", body, code, want)
		}
	}
}
