package app

import (
	"context"
	"github.com/evilperson1337/requestarr/internal/auth/oidc"
	"github.com/evilperson1337/requestarr/internal/authsettings"
	"github.com/evilperson1337/requestarr/internal/config"
	"github.com/evilperson1337/requestarr/internal/secrets"
)

// NewAuthentication builds the live authentication service independently of HTTP
// and background workers. The factory is the provider seam for deterministic tests.
func NewAuthentication(ctx context.Context, cfg config.Config, repo authsettings.Repository, factory authsettings.Factory) (*authsettings.Service, error) {
	var sealer secrets.Sealer
	if cfg.SecretsKey != "" {
		key, err := secrets.ParseMasterKey(cfg.SecretsKey)
		if err != nil {
			return nil, err
		}
		sealer, err = secrets.NewAESGCM(key)
		if err != nil {
			return nil, err
		}
	}
	if factory == nil {
		factory = func(ctx context.Context, v authsettings.Values, redirect string) (oidc.Provider, error) {
			return oidc.New(ctx, oidc.Config{Issuer: v.Issuer, ClientID: v.ClientID, ClientSecret: v.ClientSecret, AllowHTTP: cfg.Environment == "development"}, redirect)
		}
	}
	return authsettings.New(ctx, repo, sealer, authsettings.Values{Issuer: cfg.OIDCIssuer, ClientID: cfg.OIDCClientID, ClientSecret: cfg.OIDCClientSecret}, cfg.PublicURL, factory)
}
