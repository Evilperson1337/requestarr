package main

import (
	"strings"
	"testing"
)

func setupTokenEnv(t *testing.T, issuer string) {
	t.Helper()
	for k, v := range map[string]string{
		"REQUESTARR_ENV": "development", "REQUESTARR_DATABASE_URL": "",
		"REQUESTARR_DB_HOST": "127.0.0.1", "REQUESTARR_DB_PORT": "1", "REQUESTARR_DB_NAME": "n", "REQUESTARR_DB_USER": "u", "REQUESTARR_DB_PASSWORD": "canary-pw",
		"REQUESTARR_OIDC_ISSUER": issuer, "REQUESTARR_OIDC_CLIENT_ID": "c", "REQUESTARR_OIDC_CLIENT_SECRET": "s",
		"REQUESTARR_SESSION_HMAC_SECRET": "0123456789abcdef0123456789abcdef",
	} {
		t.Setenv(k, v)
	}
}

func TestSetupTokenDefaultsTheIssuerToTheConfiguredOne(t *testing.T) {
	setupTokenEnv(t, "https://id.example.test")
	err := issueSetupToken([]string{"--subject", "sub", "--stdout"})
	if err == nil || !strings.Contains(err.Error(), "database unavailable") {
		t.Fatalf("err = %v, want it to accept the configured issuer and fail connecting", err)
	}
}

func TestSetupTokenExplicitIssuerStillWorksAndIsValidated(t *testing.T) {
	setupTokenEnv(t, "https://id.example.test")
	err := issueSetupToken([]string{"--issuer", "https://other.example.test", "--subject", "sub", "--stdout"})
	if err == nil || !strings.Contains(err.Error(), "database unavailable") {
		t.Fatalf("err = %v, want explicit issuer accepted", err)
	}
	err = issueSetupToken([]string{"--issuer", "http://insecure.example.test", "--subject", "sub", "--stdout"})
	if err == nil || !strings.Contains(err.Error(), "valid --issuer") {
		t.Fatalf("err = %v, want an invalid explicit issuer rejected", err)
	}
}

func TestSetupTokenStillRejectsAnInvalidSubjectOrMissingIssuer(t *testing.T) {
	setupTokenEnv(t, "https://id.example.test")
	for _, args := range [][]string{{"--stdout"}, {"--subject", "has space", "--stdout"}} {
		if err := issueSetupToken(args); err == nil || !strings.Contains(err.Error(), "valid") {
			t.Fatalf("args %v: err = %v", args, err)
		}
	}
	setupTokenEnv(t, "")
	if err := issueSetupToken([]string{"--subject", "sub", "--stdout"}); err == nil || !strings.Contains(err.Error(), "valid --issuer") {
		t.Fatalf("no configured issuer and no flag: err = %v", err)
	}
}
