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

// byName resolves each registered secret independently.
type byName map[secrets.Name]secrets.Resolved

func (b byName) Resolve(_ context.Context, n secrets.Name) (secrets.Resolved, error) {
	return b[n], nil
}

func TestIGDBCheckUsesTheResolvedStoredSecret(t *testing.T) {
	token, platforms := fixture(t, "oauth.json"), fixture(t, "platforms.json")
	var sentSecret, sentClientID, platformClientID string
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		if calls == 1 {
			_ = r.ParseForm()
			sentSecret = r.PostForm.Get("client_secret")
			sentClientID = r.PostForm.Get("client_id")
			_, _ = w.Write(token)
			return
		}
		platformClientID = r.Header.Get("Client-ID")
		_, _ = w.Write(platforms)
	}))
	defer server.Close()
	client := &http.Client{Transport: routeTo{server.Client().Transport, server.URL}}
	settings := setup.Settings{Providers: setup.ProviderSettings{IGDBEnabled: true, IGDBClientID: "igdb-client", IGDBClientSecretEnv: setup.IGDBSecretEnv}}

	// Both the client ID and secret were saved through Setup after boot.
	checker := checks.New(checks.Config{Client: client,
		Secrets: byName{secrets.IGDBClientSecret: {Value: "stored-igdb", Source: secrets.SourceStored}}})
	code, err := checker.Check(context.Background(), setup.TargetIGDB, settings)
	if err != nil || code != setup.CheckOK || sentSecret != "stored-igdb" {
		t.Fatalf("code %q err %v secret %q", code, err, sentSecret)
	}
	if calls != 2 || sentClientID != "igdb-client" || platformClientID != "igdb-client" {
		t.Fatalf("calls=%d OAuth client=%q platform client=%q", calls, sentClientID, platformClientID)
	}
}

func TestIGDBCheckWithoutAReadableSecretNeedsTheEnvironment(t *testing.T) {
	settings := setup.Settings{Providers: setup.ProviderSettings{IGDBEnabled: true, IGDBClientID: "igdb-client", IGDBClientSecretEnv: setup.IGDBSecretEnv}}
	for name, s := range map[string]byName{
		"none":       {secrets.IGDBClientSecret: {Source: secrets.SourceNone}},
		"unreadable": {secrets.IGDBClientSecret: {Source: secrets.SourceUnreadable}},
	} {
		checker := checks.New(checks.Config{Runtime: setup.Runtime{IGDBClientID: "igdb-client"}, Secrets: s})
		if code, _ := checker.Check(context.Background(), setup.TargetIGDB, settings); code != setup.CheckEnvironment {
			t.Errorf("%s: code %q", name, code)
		}
	}
	checker := checks.New(checks.Config{Runtime: setup.Runtime{IGDBClientID: "igdb-client"}, Secrets: failingResolver{}})
	if code, _ := checker.Check(context.Background(), setup.TargetIGDB, settings); code == setup.CheckOK {
		t.Error("lookup error passed")
	}
}

type failingResolver struct{}

func (failingResolver) Resolve(context.Context, secrets.Name) (secrets.Resolved, error) {
	return secrets.Resolved{}, errors.New("db down")
}

func TestAppriseCheckAcceptsAStoredKeyWithTheEnvironmentURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("OK"))
	}))
	defer server.Close()
	runtime := setup.Runtime{AppriseURL: server.URL, Development: true} // key not configured at boot
	n := setup.NotificationSettings{AppriseEnabled: true, AppriseURL: server.URL, AppriseConfigKeyEnv: setup.AppriseConfigKeyEnv}
	run := func(s byName) setup.CheckCode {
		code, _ := checks.New(checks.Config{Runtime: runtime, Client: server.Client(), Secrets: s}).Check(context.Background(), setup.TargetNotifications, setup.Settings{Notifications: n})
		return code
	}
	if code := run(byName{secrets.AppriseConfigKey: {Value: "k", Source: secrets.SourceStored}}); code != setup.CheckOK {
		t.Fatalf("stored key: %q", code)
	}
	if code := run(byName{secrets.AppriseConfigKey: {Source: secrets.SourceNone}}); code != setup.CheckEnvironment {
		t.Fatalf("no key: %q", code)
	}
	if code := run(byName{secrets.AppriseConfigKey: {Source: secrets.SourceUnreadable}}); code != setup.CheckEnvironment {
		t.Fatalf("unreadable key: %q", code)
	}
}
