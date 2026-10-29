package app

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/evilperson1337/requestarr/internal/config"
	"github.com/evilperson1337/requestarr/internal/secrets"
)

type fakeSecretRepo struct{ rows []secrets.Stored }

func (f fakeSecretRepo) ListStoredSecrets(context.Context) ([]secrets.Stored, error) {
	return f.rows, nil
}
func (fakeSecretRepo) PutStoredSecret(context.Context, string, secrets.Name, secrets.Sealed) error {
	return nil
}
func (fakeSecretRepo) DeleteStoredSecret(context.Context, string, secrets.Name) error { return nil }

const hexKeyA = "0101010101010101010101010101010101010101010101010101010101010101"
const hexKeyB = "0202020202020202020202020202020202020202020202020202020202020202"

func storedTMDB(t *testing.T, hexKey, value string) fakeSecretRepo {
	t.Helper()
	key, _ := secrets.ParseMasterKey(hexKey)
	sealer, _ := secrets.NewAESGCM(key)
	sealed, err := sealer.Seal(secrets.TMDBReadToken, []byte(value))
	if err != nil {
		t.Fatal(err)
	}
	return fakeSecretRepo{rows: []secrets.Stored{{Name: secrets.TMDBReadToken, Sealed: sealed}}}
}

func resolve(t *testing.T, cfg config.Config, repo fakeSecretRepo) (config.Config, string) {
	t.Helper()
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	got, _, _, err := resolveSecrets(context.Background(), cfg, repo, logger)
	if err != nil {
		t.Fatalf("resolveSecrets must not fail boot: %v", err)
	}
	return got, logs.String()
}

func TestStartupAppliesTheStoredTokenWhenTheEnvironmentHasNone(t *testing.T) {
	got, _ := resolve(t, config.Config{SecretsKey: hexKeyA}, storedTMDB(t, hexKeyA, "stored-token-xyz"))
	if got.TMDBReadToken != "stored-token-xyz" {
		t.Fatal("stored token not applied")
	}
}

func TestStartupEnvironmentWinsOverStored(t *testing.T) {
	got, _ := resolve(t, config.Config{SecretsKey: hexKeyA, TMDBReadToken: "env-token"}, storedTMDB(t, hexKeyA, "stored-token-xyz"))
	if got.TMDBReadToken != "env-token" {
		t.Fatalf("token %q", got.TMDBReadToken)
	}
}

func TestStartupSurvivesWrongOrMissingKeyAndReportsUnreadableWithoutLeaking(t *testing.T) {
	for name, key := range map[string]string{"wrong key": hexKeyB, "missing key": ""} {
		got, logs := resolve(t, config.Config{SecretsKey: key}, storedTMDB(t, hexKeyA, "stored-token-xyz"))
		if got.TMDBReadToken != "" {
			t.Errorf("%s: unreadable secret produced a token", name)
		}
		if !strings.Contains(logs, "stored secret unreadable") || strings.Contains(logs, "stored-token-xyz") {
			t.Errorf("%s: logs %q", name, logs)
		}
	}
}

func storedNavidrome(t *testing.T, hexKey, value string) fakeSecretRepo {
	t.Helper()
	key, _ := secrets.ParseMasterKey(hexKey)
	sealer, _ := secrets.NewAESGCM(key)
	sealed, err := sealer.Seal(secrets.NavidromePassword, []byte(value))
	if err != nil {
		t.Fatal(err)
	}
	return fakeSecretRepo{rows: []secrets.Stored{{Name: secrets.NavidromePassword, Sealed: sealed}}}
}

func TestStoredNavidromePasswordCompletesTheUrlUsernamePasswordGroup(t *testing.T) {
	cfg := config.Config{SecretsKey: hexKeyA, NavidromeURL: "https://music.example.test", NavidromeUsername: "requestarr"}
	got, _ := resolve(t, cfg, storedNavidrome(t, hexKeyA, "stored-pass"))
	if got.NavidromePassword != "stored-pass" || got.NavidromeUsername != "requestarr" {
		t.Fatal("stored password not applied")
	}
	env := cfg
	env.NavidromePassword = "env-pass"
	if got, _ := resolve(t, env, storedNavidrome(t, hexKeyA, "stored-pass")); got.NavidromePassword != "env-pass" {
		t.Fatal("environment must win")
	}
}

func TestUnreadableNavidromePasswordLeavesNavidromeOffInsteadOfFailingValidation(t *testing.T) {
	cfg := config.Config{SecretsKey: hexKeyB, NavidromeURL: "https://music.example.test", NavidromeUsername: "requestarr"}
	got, logs := resolve(t, cfg, storedNavidrome(t, hexKeyA, "stored-pass"))
	if got.NavidromeURL != "" || got.NavidromeUsername != "" || got.NavidromePassword != "" {
		t.Fatalf("Navidrome must be left unconfigured as a unit: %+v", got)
	}
	if strings.Contains(logs, "stored-pass") || !strings.Contains(logs, "stored secret unreadable") {
		t.Fatalf("logs %q", logs)
	}
}
