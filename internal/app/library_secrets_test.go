package app

import (
	"strings"
	"testing"

	"github.com/evilperson1337/requestarr/internal/config"
	"github.com/evilperson1337/requestarr/internal/secrets"
)

func storedRows(t *testing.T, hexKey string, values map[secrets.Name]string) fakeSecretRepo {
	t.Helper()
	key, _ := secrets.ParseMasterKey(hexKey)
	sealer, _ := secrets.NewAESGCM(key)
	var repo fakeSecretRepo
	for name, value := range values {
		sealed, err := sealer.Seal(name, []byte(value))
		if err != nil {
			t.Fatal(err)
		}
		repo.rows = append(repo.rows, secrets.Stored{Name: name, Sealed: sealed})
	}
	return repo
}

func TestStoredLibraryCredentialsSatisfyTheUrlPlusSecretRule(t *testing.T) {
	cfg := validBase(config.Config{SecretsKey: hexKeyA,
		JellyfinURL: "http://jellyfin.example.test", PlexURL: "http://plex.example.test", RommURL: "http://romm.example.test"})
	repo := storedRows(t, hexKeyA, map[secrets.Name]string{
		secrets.JellyfinAPIKey: "jf-stored", secrets.PlexToken: "plex-stored", secrets.RommToken: "romm-stored"})
	got, _ := resolve(t, cfg, repo)
	if got.JellyfinAPIKey != "jf-stored" || got.PlexToken != "plex-stored" || got.RommToken != "romm-stored" {
		t.Fatal("stored library credentials not applied")
	}
	if err := got.Validate(); err != nil {
		t.Fatalf("stored-only credentials must satisfy pairing: %v", err)
	}
}

func TestEnvironmentLibraryCredentialWinsOverStored(t *testing.T) {
	cfg := config.Config{SecretsKey: hexKeyA, JellyfinURL: "http://jellyfin.example.test", JellyfinAPIKey: "jf-env"}
	got, _ := resolve(t, cfg, storedRows(t, hexKeyA, map[secrets.Name]string{secrets.JellyfinAPIKey: "jf-stored"}))
	if got.JellyfinAPIKey != "jf-env" {
		t.Fatalf("key %q", got.JellyfinAPIKey)
	}
}

func TestUnreadableStoredLibraryCredentialDoesNotStopBoot(t *testing.T) {
	// The URL is in env but the key can only come from a store that cannot be read:
	// the Source stays unconfigured (reported unreadable) instead of failing validation.
	cfg := validBase(config.Config{SecretsKey: hexKeyB, PlexURL: "http://plex.example.test"})
	got, logs := resolve(t, cfg, storedRows(t, hexKeyA, map[secrets.Name]string{secrets.PlexToken: "plex-stored"}))
	if got.PlexURL != "" || got.PlexToken != "" {
		t.Fatal("unreadable credential must leave the Source unconfigured")
	}
	if err := got.Validate(); err != nil {
		t.Fatalf("boot must continue: %v", err)
	}
	if strings.Contains(logs, "plex-stored") || !strings.Contains(logs, "plex_token") {
		t.Fatalf("logs %q", logs)
	}
}

func validBase(c config.Config) config.Config {
	c.DatabaseURL, c.Environment, c.HTTPAddr, c.LogLevelRaw = "postgres://x", "development", ":8080", "info"
	c.PublicURL, c.OIDCIssuer, c.OIDCClientID, c.OIDCClientSecret = "http://localhost:8080", "http://localhost:8081", "id", "secret"
	c.SessionHMACSecret = strings.Repeat("s", 32)
	return c
}
