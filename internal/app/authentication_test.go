package app_test

import (
	"context"
	"github.com/evilperson1337/requestarr/internal/app"
	"github.com/evilperson1337/requestarr/internal/auth/oidc"
	"github.com/evilperson1337/requestarr/internal/authsettings"
	"github.com/evilperson1337/requestarr/internal/config"
	"testing"
)

type oidcRepo struct{ record *authsettings.Record }

func (r *oidcRepo) LoadOIDC(context.Context) (*authsettings.Record, error) { return r.record, nil }
func (r *oidcRepo) SaveOIDC(_ context.Context, _ string, v *authsettings.Record) error {
	r.record = v
	return nil
}

type fakeOIDC struct{}

func (fakeOIDC) AuthorizationURL(string, string, string) string { return "https://id.test" }
func (fakeOIDC) Exchange(context.Context, string, string, string) (*oidc.Identity, error) {
	return nil, nil
}
func TestAuthenticationStartsWithoutOIDCAndUsesEnvironmentAsUnit(t *testing.T) {
	for _, mode := range []string{"none", "partial", "complete"} {
		t.Run(mode, func(t *testing.T) {
			cfg := config.Config{PublicURL: "https://requestarr.test"}
			if mode != "none" {
				cfg.OIDCClientID = "environment-id"
			}
			if mode == "complete" {
				cfg.OIDCIssuer = "https://id.test"
				cfg.OIDCClientSecret = "environment-secret"
			}
			calls := 0
			svc, err := app.NewAuthentication(context.Background(), cfg, &oidcRepo{}, func(_ context.Context, v authsettings.Values, redirect string) (oidc.Provider, error) {
				calls++
				if v.ClientID != "environment-id" || redirect != "https://requestarr.test/api/v1/auth/callback" {
					t.Error("wrong config or redirect")
				}
				return fakeOIDC{}, nil
			})
			if err != nil {
				t.Fatal("local recovery must not require OIDC")
			}
			if (svc.Snapshot().Provider != nil) != (mode == "complete") {
				t.Fatal("incorrect live OIDC state")
			}
			if mode != "complete" && calls != 0 {
				t.Fatal("incomplete settings reached discovery")
			}
		})
	}
}
