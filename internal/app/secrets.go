package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/evilperson1337/requestarr/internal/config"
	"github.com/evilperson1337/requestarr/internal/httpapi"
	"github.com/evilperson1337/requestarr/internal/secrets"
	"github.com/evilperson1337/requestarr/internal/sources"
)

// unreadableNote is shown in operational status for an integration whose stored
// secret cannot be opened, so it is never mistaken for "not configured".
const unreadableNote = "stored secret unreadable"

// secretFields maps each registered secret to its Config field. Library
// Source credentials come from the Source kind registry; the other
// integrations add a line here and a registry entry.
func secretFields(cfg *config.Config) map[secrets.Name]*string {
	fields := map[secrets.Name]*string{
		secrets.TMDBReadToken:    &cfg.TMDBReadToken,
		secrets.IGDBClientSecret: &cfg.IGDBClientSecret,
		secrets.AppriseConfigKey: &cfg.AppriseConfigKey,
	}
	for _, kind := range sources.All() {
		for _, credential := range kind.Credentials {
			if credential.Secret != "" {
				fields[secrets.Name(credential.Secret)] = cfg.LibraryCredential(kind.Slug, credential.Role)
			}
		}
	}
	return fields
}

// unreadableSource finds the Source kind whose credential is the stored secret
// name, so an unreadable secret can hold that Source off.
func unreadableSource(name secrets.Name) (sources.Kind, bool) {
	for _, kind := range sources.All() {
		for _, credential := range kind.Credentials {
			if credential.Secret == string(name) {
				return kind, true
			}
		}
	}
	return sources.Kind{}, false
}

// resolveSecrets applies D024 startup resolution: for each registered secret the
// environment value wins, otherwise the decrypted stored value is used. It runs
// before config.Validate so a stored secret can satisfy pairing rules. A stored
// secret that cannot be read leaves the integration unconfigured-but-reported
// (never guessed) and does not stop boot. Values are never logged.
func resolveSecrets(ctx context.Context, cfg config.Config, repo secrets.Repository, logger *slog.Logger) (config.Config, *secrets.Service, map[secrets.Name]string, error) {
	var sealer secrets.Sealer
	if cfg.SecretsKey != "" {
		key, err := secrets.ParseMasterKey(cfg.SecretsKey)
		if err != nil {
			return cfg, nil, nil, err
		}
		if sealer, err = secrets.NewAESGCM(key); err != nil {
			return cfg, nil, nil, err
		}
	}
	env := map[secrets.Name]string{}
	for name, field := range secretFields(&cfg) {
		env[name] = *field
	}
	svc := secrets.NewService(repo, sealer, env)
	values, err := svc.Boot(ctx)
	if err != nil {
		return cfg, nil, nil, fmt.Errorf("resolve stored secrets: %w", err)
	}
	for name, field := range secretFields(&cfg) {
		*field = values[name]
	}
	// A stored credential whose Source has no URL cannot configure it: the URL is
	// env-only. Hold the Source off rather than fail boot, which would also lock
	// the operator out of the Settings screen that stores credentials.
	for _, kind := range sources.All() {
		if *cfg.LibraryURL(kind.Slug) != "" {
			continue
		}
		for _, credential := range kind.Credentials {
			if credential.Secret == "" || env[secrets.Name(credential.Secret)] != "" {
				continue
			}
			if field := cfg.LibraryCredential(kind.Slug, credential.Role); *field != "" {
				*field = ""
				logger.Warn("stored credential ignored: the Source has no URL", "source", kind.Slug, "set", kind.URLEnv)
			}
		}
	}
	statuses, _, err := svc.Status(ctx)
	if err != nil {
		return cfg, nil, nil, fmt.Errorf("resolve stored secrets: %w", err)
	}
	unreadable := map[secrets.Name]string{}
	for _, st := range statuses {
		if st.Unreadable {
			unreadable[st.Name] = unreadableNote
			if kind, ok := unreadableSource(st.Name); ok && *cfg.LibraryCredential(kind.Slug, credentialRole(kind, st.Name)) == "" {
				// A Source's URL and credentials must be configured together;
				// leave the whole Source off rather than failing boot or guessing.
				*cfg.LibraryURL(kind.Slug) = ""
				for _, credential := range kind.Credentials {
					if credential.Secret == "" {
						*cfg.LibraryCredential(kind.Slug, credential.Role) = ""
					}
				}
			}
			logger.Warn("stored secret unreadable; the integration is not configured until the secret is re-entered or REQUESTARR_SECRETS_KEY is restored", "secret", string(st.Name))
			if st.Name == secrets.AppriseConfigKey && cfg.AppriseConfigKey == "" {
				// The URL stays env-only, but a URL without its key would fail
				// the pairing rule and stop boot; keep Apprise off instead.
				cfg.AppriseURL = ""
			}
		}
	}
	return cfg, svc, unreadable, nil
}

// credentialRole is the role of the credential field backed by a stored secret.
func credentialRole(kind sources.Kind, name secrets.Name) sources.Role {
	for _, credential := range kind.Credentials {
		if credential.Secret == string(name) {
			return credential.Role
		}
	}
	return ""
}

// The HTTP API takes the secrets service directly.
var _ httpapi.SecretsService = (*secrets.Service)(nil)
