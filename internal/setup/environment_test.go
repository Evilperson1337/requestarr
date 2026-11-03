package setup

import (
	"context"
	"testing"

	"github.com/evilperson1337/requestarr/internal/secrets"
)

func environmentItem(t *testing.T, runtime Runtime, name string) EnvironmentItem {
	t.Helper()
	svc := NewService(&memoryDrafts{}, fakeChecker{}, runtime)
	draft, err := svc.Load(context.Background(), Actor{ID: "admin-1", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range draft.Environment {
		if item.Name == name {
			return item
		}
	}
	t.Fatalf("no environment item %s", name)
	return EnvironmentItem{}
}

// The provider secrets read as configured only when a value is in effect, and
// ask for a restart otherwise.
func TestEnvironmentListingReportsProviderSecretsFromTheRuntime(t *testing.T) {
	configured := Runtime{
		IGDBClientID:      "client",
		AppriseURL:        "https://apprise.example.test",
		SecretsConfigured: map[secrets.Name]bool{secrets.TMDBReadToken: true, secrets.IGDBClientSecret: true, secrets.AppriseConfigKey: true},
	}
	for _, name := range []string{TMDBTokenEnv, IGDBSecretEnv, AppriseConfigKeyEnv} {
		if item := environmentItem(t, configured, name); !item.Secret || !item.Configured || item.NeedsRestart {
			t.Errorf("%s with a value in effect: %+v", name, item)
		}
	}
	absent := Runtime{IGDBClientID: "client", AppriseURL: "https://apprise.example.test"}
	for _, name := range []string{TMDBTokenEnv, IGDBSecretEnv} {
		if item := environmentItem(t, absent, name); item.Configured || !item.NeedsRestart {
			t.Errorf("%s with no value in effect: %+v", name, item)
		}
	}
	if item := environmentItem(t, absent, AppriseConfigKeyEnv); item.Configured {
		t.Errorf("Apprise key with no value in effect: %+v", item)
	}
}
