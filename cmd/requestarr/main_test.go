package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupTokenLoadsConfigurationFromDatabaseParts(t *testing.T) {
	for k, v := range map[string]string{
		"REQUESTARR_ENV": "development", "REQUESTARR_DATABASE_URL": "",
		"REQUESTARR_DB_HOST": "127.0.0.1", "REQUESTARR_DB_PORT": "1", "REQUESTARR_DB_NAME": "n", "REQUESTARR_DB_USER": "u", "REQUESTARR_DB_PASSWORD": "canary-pw",
		"REQUESTARR_OIDC_ISSUER": "https://id.example.test", "REQUESTARR_OIDC_CLIENT_ID": "c", "REQUESTARR_OIDC_CLIENT_SECRET": "s",
		"REQUESTARR_SESSION_HMAC_SECRET": "0123456789abcdef0123456789abcdef",
	} {
		t.Setenv(k, v)
	}
	err := issueSetupToken([]string{"--issuer", "https://id.example.test", "--subject", "sub", "--stdout"})
	if err == nil || !strings.Contains(err.Error(), "database unavailable") {
		t.Fatalf("err = %v, want it to get past configuration and fail connecting", err)
	}
	if strings.Contains(err.Error(), "canary-pw") {
		t.Fatalf("error leaks the password: %v", err)
	}
}

func TestTokenFileIsExclusiveAndPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	if err := writeTokenFile(path, "secret-token"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("mode=%o, want 0600", info.Mode().Perm())
	}
	if err := writeTokenFile(path, "replacement"); err == nil {
		t.Fatal("expected existing token file to be rejected")
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "secret-token\n" {
		t.Fatalf("contents=%q err=%v", got, err)
	}
}

func TestValidateSetupTokenIdentity(t *testing.T) {
	for _, tc := range []struct {
		issuer, subject string
		development, ok bool
	}{{"https://id.example/realms/app", "user-id", false, true}, {"http://localhost:8080", "user-id", true, true}, {"http://id.example", "user-id", true, false}, {"https://id.example/../realm", "user-id", false, false}, {"https://id.example", "user id", false, false}, {"https://id.example", "user\n", false, false}, {"https://id.example", "", false, false}} {
		if got := validIssuer(tc.issuer, tc.development) && validSubject(tc.subject); got != tc.ok {
			t.Errorf("issuer=%q subject=%q valid=%v, want %v", tc.issuer, tc.subject, got, tc.ok)
		}
	}
}
