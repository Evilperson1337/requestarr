package checks_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

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
