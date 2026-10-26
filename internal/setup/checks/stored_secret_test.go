package checks_test

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/secrets"
	"github.com/evilperson1337/requestarr/internal/setup"
	"github.com/evilperson1337/requestarr/internal/setup/checks"
)

type stubResolver struct {
	resolved secrets.Resolved
	err      error
}

func (s stubResolver) Resolve(context.Context, secrets.Name) (secrets.Resolved, error) {
	return s.resolved, s.err
}

func TestTMDBCheckUsesTheResolvedValueNotTheBootConfig(t *testing.T) {
	data := fixture(t, "tmdb.json")
	var sawAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
	}))
	defer server.Close()
	client := &http.Client{Transport: routeTo{server.Client().Transport, server.URL}}
	settings := setup.Settings{Providers: setup.ProviderSettings{TMDBEnabled: true, TMDBTokenEnv: setup.TMDBTokenEnv}}

	// Nothing was configured at boot; the stored value saved since is what the next restart uses.
	checker := checks.New(checks.Config{Client: client, Secrets: stubResolver{resolved: secrets.Resolved{Value: "stored-tmdb", Source: secrets.SourceStored}}})
	code, err := checker.Check(context.Background(), setup.TargetTMDB, settings)
	if err != nil || code != setup.CheckOK || sawAuth != "Bearer stored-tmdb" {
		t.Fatalf("code %q err %v auth %q", code, err, sawAuth)
	}
}

func TestTMDBCheckWithoutAReadableSecretNeedsTheEnvironment(t *testing.T) {
	settings := setup.Settings{Providers: setup.ProviderSettings{TMDBEnabled: true, TMDBTokenEnv: setup.TMDBTokenEnv}}
	for name, r := range map[string]stubResolver{
		"none":       {resolved: secrets.Resolved{Source: secrets.SourceNone}},
		"unreadable": {resolved: secrets.Resolved{Source: secrets.SourceUnreadable}},
		"lookup err": {err: errors.New("db down")},
	} {
		checker := checks.New(checks.Config{Secrets: r})
		code, _ := checker.Check(context.Background(), setup.TargetTMDB, settings)
		if code == setup.CheckOK {
			t.Errorf("%s: check passed without a secret", name)
		}
	}
}

func navidromeSettings(username string) setup.Settings {
	return setup.Settings{Libraries: setup.LibrariesSettings{Navidrome: setup.SourceSettings{
		Enabled: true, URL: "https://library.example", MediaTypes: []domain.MediaType{domain.MediaAlbum},
		Username: username, PasswordEnv: setup.NavidromePasswordEnv}}}
}

func TestNavidromeCheckUsesTheDraftUsernameAndTheResolvedPassword(t *testing.T) {
	var gotUser, gotToken, gotSalt string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		gotUser, gotToken, gotSalt = q.Get("u"), q.Get("t"), q.Get("s")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"subsonic-response":{"status":"ok"}}`))
	}))
	defer server.Close()
	client := &http.Client{Transport: routeTo{server.Client().Transport, server.URL}}
	// Nothing was configured at boot; the stored password saved since is what a restart uses.
	checker := checks.New(checks.Config{Runtime: setup.Runtime{Development: true}, Client: client, Secrets: stubResolver{resolved: secrets.Resolved{Value: "stored-pass", Source: secrets.SourceStored}}})
	code, err := checker.Check(context.Background(), setup.TargetNavidrome, navidromeSettings("drafted-user"))
	sum := md5.Sum([]byte("stored-pass" + gotSalt))
	if err != nil || code != setup.CheckOK || gotUser != "drafted-user" || gotToken != hex.EncodeToString(sum[:]) {
		t.Fatalf("code %q err %v user %q", code, err, gotUser)
	}
}

func TestNavidromeCheckWithoutPasswordOrUsernameDoesNotCallOut(t *testing.T) {
	for name, tc := range map[string]struct {
		user string
		r    stubResolver
	}{
		"no password": {"u", stubResolver{resolved: secrets.Resolved{Source: secrets.SourceNone}}},
		"unreadable":  {"u", stubResolver{resolved: secrets.Resolved{Source: secrets.SourceUnreadable}}},
		"lookup err":  {"u", stubResolver{err: errors.New("db down")}},
		"no username": {"", stubResolver{resolved: secrets.Resolved{Value: "p", Source: secrets.SourceStored}}},
	} {
		checker := checks.New(checks.Config{Runtime: setup.Runtime{Development: true}, Secrets: tc.r, Client: &http.Client{Transport: failTransport{t}}})
		code, _ := checker.Check(context.Background(), setup.TargetNavidrome, navidromeSettings(tc.user))
		if code == setup.CheckOK {
			t.Errorf("%s: check passed", name)
		}
	}
}

type failTransport struct{ t *testing.T }

func (f failTransport) RoundTrip(*http.Request) (*http.Response, error) {
	f.t.Error("a request was made without complete credentials")
	return nil, errors.New("blocked")
}
