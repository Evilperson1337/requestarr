package main

import (
	"os"
	"path/filepath"
	"testing"
)

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
