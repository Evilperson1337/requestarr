package config

import (
	"strings"
	"testing"
)

func TestDevelopmentLogsGate(t *testing.T) {
	for _, tc := range []struct {
		env, flag        string
		enabled, invalid bool
	}{
		{"", "", false, false},
		{"development", "", false, false},
		{"development", "true", true, false},
		{"production", "false", false, false},
		{"production", "true", true, true},
		{"staging", "1", true, true},
		{"development", "invalid-secret-canary", false, true},
	} {
		t.Run(tc.env+"/"+tc.flag, func(t *testing.T) {
			t.Setenv("REQUESTARR_ENV", tc.env)
			t.Setenv("REQUESTARR_DEV_LOGS", tc.flag)
			t.Setenv("REQUESTARR_DATABASE_URL", "postgres://test")
			t.Setenv("REQUESTARR_SECRETS_KEY", "")
			cfg := FromEnv()
			if cfg.DevelopmentLogs != tc.enabled {
				t.Fatalf("enabled = %v", cfg.DevelopmentLogs)
			}
			for _, validate := range []func() error{cfg.ValidateBootstrap, cfg.Validate} {
				err := validate()
				flagError := err != nil && strings.Contains(err.Error(), "REQUESTARR_DEV_LOGS")
				if flagError != tc.invalid || (err != nil && strings.Contains(err.Error(), "invalid-secret-canary")) {
					t.Fatalf("validation = %v", err)
				}
			}
		})
	}
}
