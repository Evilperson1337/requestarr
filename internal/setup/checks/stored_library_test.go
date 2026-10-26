package checks_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/secrets"
	"github.com/evilperson1337/requestarr/internal/setup"
	"github.com/evilperson1337/requestarr/internal/setup/checks"
)

// namedResolver returns a per-secret result so a test can prove the check asked
// for the right secret.
type namedResolver map[secrets.Name]secrets.Resolved

func (n namedResolver) Resolve(_ context.Context, name secrets.Name) (secrets.Resolved, error) {
	return n[name], nil
}

func libraryFixture(kind string) (setup.Target, setup.LibrariesSettings, string, func(*http.Request) string) {
	source := setup.SourceSettings{Enabled: true, URL: "https://library.example", MediaTypes: []domain.MediaType{domain.MediaMovie}}
	var libs setup.LibrariesSettings
	switch kind {
	case "jellyfin":
		source.TokenEnv = setup.JellyfinAPIKeyEnv
		libs.Jellyfin = source
		return setup.TargetJellyfin, libs, `{"ServerName":"t"}`, func(r *http.Request) string { return r.Header.Get("X-Emby-Token") }
	case "plex":
		source.TokenEnv = setup.PlexTokenEnv
		libs.Plex = source
		return setup.TargetPlex, libs, `<MediaContainer size="1"/>`, func(r *http.Request) string { return r.Header.Get("X-Plex-Token") }
	}
	source.TokenEnv = setup.RommTokenEnv
	libs.Romm = source
	return setup.TargetRomm, libs, `[]`, func(r *http.Request) string { return r.Header.Get("Authorization") }
}

func TestLibraryCheckUsesTheResolvedStoredCredential(t *testing.T) {
	for _, tc := range []struct {
		kind   string
		secret secrets.Name
		want   string
	}{
		{"jellyfin", secrets.JellyfinAPIKey, "stored-value"},
		{"plex", secrets.PlexToken, "stored-value"},
		{"romm", secrets.RommToken, "Bearer stored-value"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			target, libs, body, credential := libraryFixture(tc.kind)
			var saw string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				saw = credential(r)
				w.Header().Set("Content-Type", "application/json")
				if tc.kind == "plex" {
					w.Header().Set("Content-Type", "application/xml")
				}
				_, _ = io.WriteString(w, body)
			}))
			defer server.Close()
			client := &http.Client{Transport: routeTo{server.Client().Transport, server.URL}}
			// Nothing was configured at boot; the stored value is what a restart would use.
			resolver := namedResolver{tc.secret: {Value: "stored-value", Source: secrets.SourceStored}}
			checker := checks.New(checks.Config{Runtime: setup.Runtime{Development: true}, Client: client, Secrets: resolver})
			code, err := checker.Check(context.Background(), target, setup.Settings{Libraries: libs})
			if err != nil || code != setup.CheckOK || saw != tc.want {
				t.Fatalf("code %q err %v credential %q", code, err, saw)
			}
		})
	}
}

func TestLibraryCheckWithoutAReadableCredentialNeedsTheEnvironment(t *testing.T) {
	for name, r := range map[string]secrets.Resolved{
		"none":       {Source: secrets.SourceNone},
		"unreadable": {Source: secrets.SourceUnreadable},
	} {
		target, libs, _, _ := libraryFixture("jellyfin")
		checker := checks.New(checks.Config{Runtime: setup.Runtime{Development: true}, JellyfinAPIKey: "boot-value", Secrets: namedResolver{secrets.JellyfinAPIKey: r}})
		code, _ := checker.Check(context.Background(), target, setup.Settings{Libraries: libs})
		if code == setup.CheckOK {
			t.Errorf("%s: check passed without a usable credential", name)
		}
	}
}
