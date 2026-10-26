package app

import (
	"strings"
	"testing"

	"github.com/evilperson1337/requestarr/internal/config"
	"github.com/evilperson1337/requestarr/internal/secrets"
)

func storedAs(t *testing.T, hexKey string, values map[secrets.Name]string) fakeSecretRepo {
	t.Helper()
	key, _ := secrets.ParseMasterKey(hexKey)
	sealer, _ := secrets.NewAESGCM(key)
	var repo fakeSecretRepo
	for name, v := range values {
		sealed, err := sealer.Seal(name, []byte(v))
		if err != nil {
			t.Fatal(err)
		}
		repo.rows = append(repo.rows, secrets.Stored{Name: name, Sealed: sealed})
	}
	return repo
}

func TestStartupAppliesStoredIGDBSecretAndAppriseKey(t *testing.T) {
	repo := storedAs(t, hexKeyA, map[secrets.Name]string{secrets.IGDBClientSecret: "igdb-stored", secrets.AppriseConfigKey: "apprise-stored"})
	got, _ := resolve(t, config.Config{SecretsKey: hexKeyA}, repo)
	if got.IGDBClientSecret != "igdb-stored" || got.AppriseConfigKey != "apprise-stored" {
		t.Fatalf("stored secrets not applied: %q %q", got.IGDBClientSecret, got.AppriseConfigKey)
	}
}

func TestStoredAppriseKeyWithEnvURLPassesPairingValidation(t *testing.T) {
	cfg := config.Config{SecretsKey: hexKeyA, AppriseURL: "https://apprise.example"}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "REQUESTARR_APPRISE_CONFIG_KEY") {
		t.Fatalf("precondition: URL without key must fail the pairing rule, got %v", err)
	}
	got, _ := resolve(t, cfg, storedAs(t, hexKeyA, map[secrets.Name]string{secrets.AppriseConfigKey: "apprise-stored"}))
	if err := got.Validate(); err != nil && strings.Contains(err.Error(), "configured together") {
		t.Fatalf("stored key should satisfy pairing: %v", err)
	}
}

func TestUnreadableAppriseKeyDoesNotStopBootWithTheEnvironmentURL(t *testing.T) {
	cfg := config.Config{SecretsKey: hexKeyB, AppriseURL: "https://apprise.example"}
	got, logs := resolve(t, cfg, storedAs(t, hexKeyA, map[secrets.Name]string{secrets.AppriseConfigKey: "apprise-stored"}))
	if err := got.Validate(); err != nil && strings.Contains(err.Error(), "configured together") {
		t.Fatalf("unreadable stored key must not fail the pairing rule at boot: %v", err)
	}
	if got.AppriseURL != "" || got.AppriseConfigKey != "" {
		t.Fatal("Apprise must stay off, not half configured, while its key is unreadable")
	}
	if !strings.Contains(logs, "stored secret unreadable") || strings.Contains(logs, "apprise-stored") {
		t.Fatalf("logs %q", logs)
	}
}

func TestEnvironmentWinsForIGDBAndApprise(t *testing.T) {
	repo := storedAs(t, hexKeyA, map[secrets.Name]string{secrets.IGDBClientSecret: "igdb-stored", secrets.AppriseConfigKey: "apprise-stored"})
	got, _ := resolve(t, config.Config{SecretsKey: hexKeyA, IGDBClientSecret: "igdb-env", AppriseConfigKey: "apprise-env"}, repo)
	if got.IGDBClientSecret != "igdb-env" || got.AppriseConfigKey != "apprise-env" {
		t.Fatalf("env must win: %q %q", got.IGDBClientSecret, got.AppriseConfigKey)
	}
}
