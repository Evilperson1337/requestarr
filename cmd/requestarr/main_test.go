package main

import (
	"strings"
	"testing"
)

func TestResetAdminPasswordLoadsConfigurationFromDatabaseParts(t *testing.T) {
	for k, v := range map[string]string{
		"REQUESTARR_ENV": "development", "REQUESTARR_DATABASE_URL": "",
		"REQUESTARR_DB_HOST": "127.0.0.1", "REQUESTARR_DB_PORT": "1", "REQUESTARR_DB_NAME": "n", "REQUESTARR_DB_USER": "u", "REQUESTARR_DB_PASSWORD": "canary-pw",
		"REQUESTARR_OIDC_ISSUER": "https://id.example.test", "REQUESTARR_OIDC_CLIENT_ID": "c", "REQUESTARR_OIDC_CLIENT_SECRET": "s",
		"REQUESTARR_SESSION_HMAC_SECRET": "0123456789abcdef0123456789abcdef",
	} {
		t.Setenv(k, v)
	}
	err := resetAdminPassword(nil)
	if err == nil || !strings.Contains(err.Error(), "database unavailable") {
		t.Fatal("expected reset to get past configuration and fail connecting")
	}
	if strings.Contains(err.Error(), "canary-pw") {
		t.Fatal("reset error leaks database password")
	}
}
