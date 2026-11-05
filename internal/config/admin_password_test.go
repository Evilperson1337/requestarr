package config

import (
	"strings"
	"testing"
)

func TestAdminPasswordFromEnvironmentIsValidatedWithoutEchoingIt(t *testing.T) {
	for _, tc := range []struct {
		name, pw string
		ok       bool
	}{
		{"unset", "", true},
		{"12 characters", strings.Repeat("p", 12), true},
		{"128 characters", strings.Repeat("p", 128), true},
		{"11 characters", strings.Repeat("p", 11), false},
		{"129 characters", strings.Repeat("p", 129), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("REQUESTARR_ADMIN_PASSWORD", tc.pw)
			c := FromEnv()
			err := c.ValidateBootstrap()
			if tc.ok && err != nil && strings.Contains(err.Error(), "REQUESTARR_ADMIN_PASSWORD") {
				t.Fatalf("unexpected error %v", err)
			}
			if !tc.ok {
				if err == nil || !strings.Contains(err.Error(), "REQUESTARR_ADMIN_PASSWORD") {
					t.Fatalf("err=%v, want one naming the variable", err)
				}
				if strings.Contains(err.Error(), tc.pw) {
					t.Fatal("the error echoes the password")
				}
			}
		})
	}
}
