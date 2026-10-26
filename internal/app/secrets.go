package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/evilperson1337/requestarr/internal/config"
	"github.com/evilperson1337/requestarr/internal/secrets"
	"github.com/evilperson1337/requestarr/internal/setup"
)

// unreadableNote is shown in operational status for an integration whose stored
// secret cannot be opened, so it is never mistaken for "not configured".
const unreadableNote = "stored secret unreadable"

// secretFields maps each registered secret to its Config field. Later
// integrations add a line here and a registry entry; nothing else changes.
func secretFields(cfg *config.Config) map[secrets.Name]*string {
	return map[secrets.Name]*string{
		secrets.TMDBReadToken:    &cfg.TMDBReadToken,
		secrets.IGDBClientSecret: &cfg.IGDBClientSecret,
		secrets.AppriseConfigKey: &cfg.AppriseConfigKey,
	}
}

// resolveSecrets applies D024 startup resolution: for each registered secret the
// environment value wins, otherwise the decrypted stored value is used. It runs
// before config.Validate so a stored secret can satisfy pairing rules. A stored
// secret that cannot be read leaves the integration unconfigured-but-reported
// (never guessed) and does not stop boot. Values are never logged.
func resolveSecrets(ctx context.Context, cfg config.Config, repo secrets.Repository, logger *slog.Logger) (config.Config, *secrets.Service, error) {
	var sealer secrets.Sealer
	if cfg.SecretsKey != "" {
		key, err := secrets.ParseMasterKey(cfg.SecretsKey)
		if err != nil {
			return cfg, nil, err
		}
		if sealer, err = secrets.NewAESGCM(key); err != nil {
			return cfg, nil, err
		}
	}
	env := map[secrets.Name]string{}
	for name, field := range secretFields(&cfg) {
		env[name] = *field
	}
	svc := secrets.NewService(repo, sealer, env)
	values, err := svc.Boot(ctx)
	if err != nil {
		return cfg, nil, fmt.Errorf("resolve stored secrets: %w", err)
	}
	for name, field := range secretFields(&cfg) {
		*field = values[name]
	}
	statuses, _, err := svc.Status(ctx)
	if err != nil {
		return cfg, nil, fmt.Errorf("resolve stored secrets: %w", err)
	}
	for _, st := range statuses {
		if st.Unreadable {
			logger.Warn("stored secret unreadable; the integration is not configured until the secret is re-entered or REQUESTARR_SECRETS_KEY is restored", "secret", string(st.Name))
			if st.Name == secrets.AppriseConfigKey && cfg.AppriseConfigKey == "" {
				// The URL stays env-only, but a URL without its key would fail
				// the pairing rule and stop boot; keep Apprise off instead.
				cfg.AppriseURL = ""
			}
		}
	}
	return cfg, svc, nil
}

// setupWithSecrets exposes the setup wizard and the secrets service as one
// value for the HTTP layer, which discovers the secrets routes by interface.
type setupWithSecrets struct {
	*setup.Service
	secretsAPI
}

type secretsAPI struct{ svc *secrets.Service }

func (s secretsAPI) Status(ctx context.Context) ([]secrets.Status, bool, error) {
	return s.svc.Status(ctx)
}
func (s secretsAPI) Set(ctx context.Context, actorID string, name secrets.Name, value string) error {
	return s.svc.Set(ctx, actorID, name, value)
}
func (s secretsAPI) Clear(ctx context.Context, actorID string, name secrets.Name) error {
	return s.svc.Clear(ctx, actorID, name)
}
