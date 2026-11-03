package checks_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/secrets"
	"github.com/evilperson1337/requestarr/internal/setup"
	"github.com/evilperson1337/requestarr/internal/setup/checks"
)

func readyConfig() checks.Config {
	return checks.Config{Runtime: setup.Runtime{PublicURL: "https://requestarr.example", OIDCIssuer: "https://identity.example/realm", OIDCClientID: "client", IGDBClientID: "client", OIDCSecretConfigured: true}, OIDCClientSecret: "oidc-secret", Secrets: secrets.Static{secrets.TMDBReadToken: "tmdb-secret", secrets.IGDBClientSecret: "igdb-secret"}}
}

func readySettings() setup.Settings {
	return setup.Settings{
		General:        setup.GeneralSettings{InstanceName: "Requestarr", Timezone: "UTC", PublicURL: "https://requestarr.example"},
		Authentication: setup.AuthenticationSettings{Issuer: "https://identity.example/realm", ClientID: "client", ClientSecretEnv: setup.OIDCSecretEnv},
		Providers:      setup.ProviderSettings{TMDBEnabled: true, TMDBTokenEnv: setup.TMDBTokenEnv, MusicBrainzEnabled: true, MusicBrainzContact: "admin@example.org", IGDBEnabled: true, IGDBClientID: "client", IGDBClientSecretEnv: setup.IGDBSecretEnv},
	}
}

func TestUntrustedResponses(t *testing.T) {
	for _, target := range []setup.Target{setup.TargetGeneral, setup.TargetAuthentication, setup.TargetTMDB, setup.TargetMusicBrainz, setup.TargetIGDB} {
		for _, failure := range []struct {
			name              string
			status            int
			contentType, body string
		}{
			{"credentials401", 401, "application/json", `{"error":"tmdb-secret igdb-secret oidc-secret"}`},
			{"malformed", 200, "application/json", string(fixture(t, "malformed.json"))},
			{"truncated", 200, "application/json", string(fixture(t, "truncated.json"))},
			{"oversized", 200, "application/json", `{}` + strings.Repeat(" ", 1<<20)},
			{"wrong-content-type", 200, "text/html", `{}`},
			{"missing-content-type", 200, "", `{}`},
			{"wrong-shape", 200, "application/json", `{}`},
			{"null", 200, "application/json", `null`},
			{"multiple-json-values", 200, "application/json", `{} {}`},
			{"redirect", 302, "application/json", `{}`},
		} {
			t.Run(string(target)+"/"+failure.name, func(t *testing.T) {
				calls := 0
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls++
					w.Header()["Content-Type"] = []string{failure.contentType}
					w.Header().Set("Location", "/would-leak-credentials")
					w.WriteHeader(failure.status)
					_, _ = w.Write([]byte(failure.body))
				}))
				defer server.Close()
				config := readyConfig()
				config.Client = &http.Client{Transport: routeTo{server.Client().Transport, server.URL}}
				code, err := checks.New(config).Check(context.Background(), target, readySettings())
				if target == setup.TargetTMDB && failure.status == http.StatusUnauthorized {
					if code != setup.CheckAuthentication || err != nil || calls != 1 {
						t.Fatalf("authentication rejection = %q, %v; calls = %d", code, err, calls)
					}
					return
				}
				if code != setup.CheckUnavailable || err == nil || err.Error() != "setup connection check failed" || calls != 1 {
					t.Fatalf("Check = %q, %v; calls = %d", code, err, calls)
				}
			})
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestPrerequisitesNeverMakeRequests(t *testing.T) {
	for _, tc := range []struct {
		name   string
		target setup.Target
		change func(*checks.Config, *setup.Settings)
		want   setup.CheckCode
	}{
		{"general-name", setup.TargetGeneral, func(_ *checks.Config, s *setup.Settings) { s.General.InstanceName = "" }, setup.CheckIncomplete},
		{"general-timezone", setup.TargetGeneral, func(_ *checks.Config, s *setup.Settings) { s.General.Timezone = "" }, setup.CheckIncomplete},
		{"general-url", setup.TargetGeneral, func(_ *checks.Config, s *setup.Settings) { s.General.PublicURL = "" }, setup.CheckIncomplete},
		{"general-changed", setup.TargetGeneral, func(_ *checks.Config, s *setup.Settings) { s.General.PublicURL = "https://new.example" }, setup.CheckEnvironment},
		{"invalid-timezone", setup.TargetGeneral, func(_ *checks.Config, s *setup.Settings) { s.General.Timezone = "Not/AZone" }, setup.CheckUnavailable},
		{"invalid-url", setup.TargetGeneral, func(_ *checks.Config, s *setup.Settings) { s.General.PublicURL = "https://user:secret@host" }, setup.CheckUnavailable},
		{"issuer", setup.TargetAuthentication, func(_ *checks.Config, s *setup.Settings) { s.Authentication.Issuer = "" }, setup.CheckIncomplete},
		{"oidc-client", setup.TargetAuthentication, func(_ *checks.Config, s *setup.Settings) { s.Authentication.ClientID = "" }, setup.CheckIncomplete},
		{"oidc-env-allowlist", setup.TargetAuthentication, func(_ *checks.Config, s *setup.Settings) { s.Authentication.ClientSecretEnv = "ARBITRARY" }, setup.CheckEnvironment},
		{"disabled-tmdb", setup.TargetTMDB, func(_ *checks.Config, s *setup.Settings) { s.Providers.TMDBEnabled = false }, setup.CheckDisabled},
		{"tmdb-token", setup.TargetTMDB, func(c *checks.Config, _ *setup.Settings) {
			c.Secrets = secrets.Static{secrets.IGDBClientSecret: "igdb-secret"}
		}, setup.CheckEnvironment},
		{"tmdb-env-allowlist", setup.TargetTMDB, func(_ *checks.Config, s *setup.Settings) { s.Providers.TMDBTokenEnv = "ARBITRARY" }, setup.CheckEnvironment},
		{"disabled-musicbrainz", setup.TargetMusicBrainz, func(_ *checks.Config, s *setup.Settings) { s.Providers.MusicBrainzEnabled = false }, setup.CheckDisabled},
		{"missing-contact", setup.TargetMusicBrainz, func(_ *checks.Config, s *setup.Settings) { s.Providers.MusicBrainzContact = "" }, setup.CheckIncomplete},
		{"invalid-contact", setup.TargetMusicBrainz, func(_ *checks.Config, s *setup.Settings) { s.Providers.MusicBrainzContact = "name <admin@example.org>" }, setup.CheckIncomplete},
		{"contact-header-injection", setup.TargetMusicBrainz, func(_ *checks.Config, s *setup.Settings) {
			s.Providers.MusicBrainzContact = "admin@example.org\r\nSecret: x"
		}, setup.CheckIncomplete},
		{"disabled-igdb", setup.TargetIGDB, func(_ *checks.Config, s *setup.Settings) { s.Providers.IGDBEnabled = false }, setup.CheckDisabled},
		{"igdb-client", setup.TargetIGDB, func(_ *checks.Config, s *setup.Settings) { s.Providers.IGDBClientID = "" }, setup.CheckIncomplete},
		{"igdb-secret", setup.TargetIGDB, func(c *checks.Config, _ *setup.Settings) {
			c.Secrets = secrets.Static{secrets.TMDBReadToken: "tmdb-secret"}
		}, setup.CheckEnvironment},
		{"igdb-runtime-client", setup.TargetIGDB, func(c *checks.Config, _ *setup.Settings) {
			c.Runtime.IGDBClientID = "different"
			c.Runtime.IGDBClientIDFromEnvironment = true
		}, setup.CheckEnvironment},
		{"igdb-env-allowlist", setup.TargetIGDB, func(_ *checks.Config, s *setup.Settings) { s.Providers.IGDBClientSecretEnv = "ARBITRARY" }, setup.CheckEnvironment},
		{"unknown-target", setup.Target("unknown"), func(_ *checks.Config, _ *setup.Settings) {}, setup.CheckUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config, settings := readyConfig(), readySettings()
			config.Client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				t.Error("unexpected network request")
				return nil, errors.New("secret transport error")
			})}
			tc.change(&config, &settings)
			code, err := checks.New(config).Check(context.Background(), tc.target, settings)
			if code != tc.want || (err != nil) != (tc.want == setup.CheckUnavailable) {
				t.Fatalf("Check = %q, %v; want %q", code, err, tc.want)
			}
		})
	}
}

func TestCancellationAndSanitizedTransportErrors(t *testing.T) {
	for _, target := range []setup.Target{setup.TargetGeneral, setup.TargetAuthentication, setup.TargetTMDB, setup.TargetMusicBrainz, setup.TargetIGDB} {
		t.Run(string(target), func(t *testing.T) {
			config := readyConfig()
			started := make(chan struct{})
			config.Client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				close(started)
				<-r.Context().Done()
				return nil, errors.New("https://user:secret@host/?token=tmdb-secret")
			})}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan struct{})
			go func() { defer close(done); <-started; cancel() }()
			before := time.Now()
			code, err := checks.New(config).Check(ctx, target, readySettings())
			<-done
			if code != setup.CheckUnavailable || err == nil || err.Error() != "setup connection check failed" || time.Since(before) > time.Second {
				t.Fatalf("Check = %q, %v", code, err)
			}
		})
	}
}
