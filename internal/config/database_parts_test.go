package config

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

const canaryPassword = "canary-p@ss:w/rd?#% x"

func setDBEnv(t *testing.T, vars map[string]string) {
	t.Helper()
	for _, k := range []string{"URL", "HOST", "PORT", "NAME", "USER", "PASSWORD", "SSLMODE"} {
		name := "REQUESTARR_DB_" + k
		if k == "URL" {
			name = "REQUESTARR_DATABASE_URL"
		}
		t.Setenv(name, vars[k])
	}
}

func TestDatabaseURLFromParts(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"parts build the exact url", map[string]string{"HOST": "db", "PORT": "5433", "NAME": "requestarr", "USER": "app", "PASSWORD": "pw", "SSLMODE": "require"},
			"postgres://app:pw@db:5433/requestarr?sslmode=require"},
		{"default port and no sslmode", map[string]string{"HOST": "db", "NAME": "requestarr", "USER": "app", "PASSWORD": "pw"},
			"postgres://app:pw@db:5432/requestarr"},
		{"ipv6 host", map[string]string{"HOST": "::1", "NAME": "requestarr", "USER": "app", "PASSWORD": "pw"},
			"postgres://app:pw@[::1]:5432/requestarr"},
		{"url wins over parts", map[string]string{"URL": "postgres://u:p@elsewhere/x", "HOST": "db", "NAME": "n", "USER": "u", "PASSWORD": "pw", "PORT": "0"},
			"postgres://u:p@elsewhere/x"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setDBEnv(t, tc.env)
			cfg := FromEnv()
			if cfg.DatabaseURL != tc.want {
				t.Fatalf("DatabaseURL = %q, want %q", cfg.DatabaseURL, tc.want)
			}
			if err := cfg.ValidateBootstrap(); err != nil {
				t.Fatalf("ValidateBootstrap: %v", err)
			}
		})
	}
}

func TestDatabaseURLPartsSpecialCharactersRoundTrip(t *testing.T) {
	for _, user := range []string{"app", "a@b:c/d?e#f%g h"} {
		setDBEnv(t, map[string]string{"HOST": "db", "NAME": "requestarr", "USER": user, "PASSWORD": canaryPassword})
		cfg := FromEnv()
		parsed, err := pgx.ParseConfig(cfg.DatabaseURL)
		if err != nil {
			t.Fatalf("pgx rejected the url: %v", err)
		}
		if parsed.User != user || parsed.Password != canaryPassword || parsed.Host != "db" || parsed.Database != "requestarr" {
			t.Fatalf("round trip lost data: user=%q host=%q db=%q passwordOK=%v", parsed.User, parsed.Host, parsed.Database, parsed.Password == canaryPassword)
		}
	}
}

func TestDatabasePartsProblems(t *testing.T) {
	good := func(mut map[string]string) map[string]string {
		m := map[string]string{"HOST": "db", "NAME": "n", "USER": "u", "PASSWORD": canaryPassword}
		for k, v := range mut {
			m[k] = v
		}
		return m
	}
	tests := []struct {
		name string
		env  map[string]string
		want []string
	}{
		{"nothing set names both options", map[string]string{}, []string{"REQUESTARR_DATABASE_URL", "REQUESTARR_DB_HOST"}},
		{"missing host", good(map[string]string{"HOST": ""}), []string{"REQUESTARR_DB_HOST"}},
		{"missing name", good(map[string]string{"NAME": ""}), []string{"REQUESTARR_DB_NAME"}},
		{"missing user", good(map[string]string{"USER": ""}), []string{"REQUESTARR_DB_USER"}},
		{"missing password", good(map[string]string{"PASSWORD": ""}), []string{"REQUESTARR_DB_PASSWORD"}},
		{"port text", good(map[string]string{"PORT": "abc-" + canaryPassword}), []string{"REQUESTARR_DB_PORT"}},
		{"port zero", good(map[string]string{"PORT": "0"}), []string{"REQUESTARR_DB_PORT"}},
		{"port too big", good(map[string]string{"PORT": "65536"}), []string{"REQUESTARR_DB_PORT"}},
		{"bad sslmode", good(map[string]string{"SSLMODE": "yes-" + canaryPassword}), []string{"REQUESTARR_DB_SSLMODE"}},
		{"host with space", good(map[string]string{"HOST": "d b"}), []string{"REQUESTARR_DB_HOST"}},
		{"host with slash", good(map[string]string{"HOST": "db/x"}), []string{"REQUESTARR_DB_HOST"}},
		{"host with at", good(map[string]string{"HOST": "u@db"}), []string{"REQUESTARR_DB_HOST"}},
		{"host with question", good(map[string]string{"HOST": "db?x"}), []string{"REQUESTARR_DB_HOST"}},
		{"host with hash", good(map[string]string{"HOST": "db#x"}), []string{"REQUESTARR_DB_HOST"}},
		{"several at once", map[string]string{"HOST": "db"}, []string{"REQUESTARR_DB_NAME", "REQUESTARR_DB_USER", "REQUESTARR_DB_PASSWORD"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setDBEnv(t, tc.env)
			cfg := FromEnv()
			for name, validate := range map[string]func() error{"bootstrap": cfg.ValidateBootstrap, "full": cfg.Validate} {
				err := validate()
				if err == nil {
					t.Fatalf("%s: want an error", name)
				}
				for _, w := range tc.want {
					if !strings.Contains(err.Error(), w) {
						t.Errorf("%s: %q does not name %s", name, err, w)
					}
				}
				for _, secret := range []string{canaryPassword, "canary-p", "yes-", "abc-"} {
					if strings.Contains(err.Error(), secret) {
						t.Errorf("%s: error echoes a value: %q", name, err)
					}
				}
			}
		})
	}
}
