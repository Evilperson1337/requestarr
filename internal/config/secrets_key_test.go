package config

import (
	"strings"
	"testing"
)

func TestSecretsKeyValidation(t *testing.T) {
	good := strings.Repeat("ab", 32) // 64 hex characters
	cases := []struct {
		name    string
		key     string
		wantErr bool
	}{
		{"absent is allowed", "", false},
		{"valid hex", good, false},
		{"malformed", "too-short", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Config{DatabaseURL: validDBURL, SecretsKey: tc.key}.ValidateBootstrap()
			if (err != nil) != tc.wantErr {
				t.Fatalf("ValidateBootstrap() = %v, wantErr %v", err, tc.wantErr)
			}
			if err != nil && strings.Contains(err.Error(), tc.key) {
				t.Fatalf("error echoes the key: %v", err)
			}
		})
	}
}

func TestFullValidateAlsoRejectsMalformedSecretsKey(t *testing.T) {
	cfg := FromEnv()
	cfg.SecretsKey = "too-short"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "REQUESTARR_SECRETS_KEY") {
		t.Fatalf("Validate() = %v", err)
	}
}

func TestValidateBootstrapRequiresDatabaseURL(t *testing.T) {
	if err := (Config{}).ValidateBootstrap(); err == nil {
		t.Fatal("want error for missing database url")
	}
}

func TestFromEnvReadsSecretsKey(t *testing.T) {
	t.Setenv("REQUESTARR_SECRETS_KEY", "k")
	if got := FromEnv().SecretsKey; got != "k" {
		t.Fatalf("SecretsKey = %q", got)
	}
}
