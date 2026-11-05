package config

import "testing"

func TestOIDCOptionalAtStartup(t *testing.T) {
	t.Setenv("REQUESTARR_DATABASE_URL", "postgres://db.test/requestarr")
	t.Setenv("REQUESTARR_SESSION_HMAC_SECRET", "01234567890123456789012345678901")
	t.Setenv("REQUESTARR_PUBLIC_URL", "https://requestarr.test")
	for _, id := range []string{"", "partial"} {
		t.Setenv("REQUESTARR_OIDC_ISSUER", "")
		t.Setenv("REQUESTARR_OIDC_CLIENT_ID", id)
		t.Setenv("REQUESTARR_OIDC_CLIENT_SECRET", "")
		if err := FromEnv().Validate(); err != nil {
			t.Fatal("absent or incomplete OIDC must not prevent local recovery")
		}
	}
}
