package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/evilperson1337/requestarr/internal/config"
	"github.com/evilperson1337/requestarr/internal/inventory"
	"github.com/evilperson1337/requestarr/internal/opstatus"
	"github.com/evilperson1337/requestarr/internal/secrets"
	"github.com/evilperson1337/requestarr/internal/setup"
	"github.com/evilperson1337/requestarr/internal/sources"
)

// bootStore is what startup resolution reads from the database.
type bootStore interface {
	igdbSettingsStore
	connectionStore
	secrets.Repository
}

// booted is the configuration after startup resolution, with what resolution
// learned along the way.
type booted struct {
	Config  config.Config
	Secrets *secrets.Service
	// Unreadable names each stored secret that could not be opened.
	Unreadable map[secrets.Name]string
	// IGDBClientIDFromEnvironment is true when the operator set the client ID,
	// as opposed to it being loaded from the saved setting.
	IGDBClientIDFromEnvironment bool
	// Connections says where each library Source's URL came from.
	Connections map[string]connectionOrigin
}

// resolveBoot is the one place the startup order lives: the saved IGDB client
// ID first (it needs the environment value untouched), then secrets (environment,
// else stored), and only then validation, so a stored secret can satisfy the
// pairing rules.
func resolveBoot(ctx context.Context, cfg config.Config, store bootStore, logger *slog.Logger) (booted, error) {
	b, err := resolveConfig(ctx, cfg, store, logger)
	if err != nil {
		return booted{}, err
	}
	if err := withoutIncompleteSavedSources(b).Validate(); err != nil {
		return booted{}, fmt.Errorf("invalid configuration: %w", err)
	}
	return b, nil
}

// withoutIncompleteSavedSources is the configuration to validate: a library
// Source whose address was saved in Settings but lacks its credential is left
// out. That is an ordinary state (the credential was never entered, or was
// cleared), which Settings, Integrations reports as missing and the Source
// manager holds off. Refusing to start over it would also lock the operator out
// of the screen that fixes it. An address set by an environment variable stays
// subject to the pairing rule: that is a mistake in the deployment itself.
func withoutIncompleteSavedSources(b booted) config.Config {
	check := b.Config
	for _, kind := range sources.All() {
		if b.Connections[kind.Slug].FromEnvironment || len(check.LibraryProblems(kind.Slug)) == 0 {
			continue
		}
		*check.LibraryURL(kind.Slug) = ""
		for _, credential := range kind.Credentials {
			*check.LibraryCredential(kind.Slug, credential.Role) = ""
		}
	}
	return check
}

// resolveConfig is resolveBoot without validation, so one Source can be
// re-resolved and checked on its own while the server runs.
func resolveConfig(ctx context.Context, cfg config.Config, store bootStore, logger *slog.Logger) (booted, error) {
	fromEnvironment := cfg.IGDBClientID != ""
	cfg, err := resolveIGDBClientID(ctx, cfg, store)
	if err != nil {
		return booted{}, fmt.Errorf("IGDB settings startup: %w", err)
	}
	cfg, origins, err := resolveLibraryConnections(ctx, cfg, store)
	if err != nil {
		return booted{}, fmt.Errorf("library connections startup: %w", err)
	}
	cfg, svc, unreadable, err := resolveSecrets(ctx, cfg, store, logger)
	if err != nil {
		return booted{}, fmt.Errorf("secrets startup: %w", err)
	}
	return booted{Config: cfg, Secrets: svc, Unreadable: unreadable, IGDBClientIDFromEnvironment: fromEnvironment, Connections: origins}, nil
}

// sourceDecorator wraps a Source's sync job, for work that belongs to one kind.
type sourceDecorator func(func(context.Context) error) func(context.Context) error

// Adapters are the outside pieces Build needs. Tests supply fakes.
type Adapters struct {
	// NewSource builds the inventory client for a configured Source kind.
	NewSource func(config.Config, sources.Kind) (inventory.Source, sourceDecorator, error)
	// SyncStore receives snapshots and sync failures.
	SyncStore sourceSyncStore
	Logger    *slog.Logger
}

// SourceJob is one configured Source's sync job and its stable ID.
type SourceJob struct {
	Slug   string
	ID     string
	Job    func(context.Context) error
	Client inventory.Source
}

// Runtime describes what a running server is made of, derived from the
// resolved configuration alone. Run only starts it.
type Runtime struct {
	Jobs      []SourceJob
	Providers []opstatus.Provider
	Sources   []opstatus.SourceRef
	Setup     setup.Runtime
}

// Build derives the Runtime. It performs no I/O: Sources are constructed
// through the adapters but nothing is started.
func Build(b booted, a Adapters) (Runtime, error) {
	cfg := b.Config
	var rt Runtime
	jobs := map[string]syncSource{}
	setupURLs := map[string]string{}
	usernames := map[string]string{}
	for _, kind := range sources.All() {
		baseURL := *cfg.LibraryURL(kind.Slug)
		setupURLs[kind.Slug] = baseURL
		if _, ok := kind.Credential(sources.RoleUsername); ok {
			usernames[kind.Slug] = *cfg.LibraryCredential(kind.Slug, sources.RoleUsername)
		}
		if baseURL == "" {
			continue
		}
		client, decorate, err := a.NewSource(cfg, kind)
		if err != nil {
			return Runtime{}, fmt.Errorf("initialize %s integration: %w", kind.Name, err)
		}
		job := sourceSyncJob(a.Logger, a.SyncStore, client, baseURL)
		if decorate != nil {
			job = decorate(job)
		}
		id := client.Descriptor().ID
		rt.Jobs = append(rt.Jobs, SourceJob{Slug: kind.Slug, ID: id, Job: job, Client: client})
		jobs[kind.Slug] = syncSource{id: id, job: job}
	}
	rt.Sources = sourceRefs(jobs, b.Unreadable)
	rt.Providers = providerStatuses(cfg, b.Unreadable)
	rt.Setup = setup.Runtime{
		PublicURL: cfg.PublicURL, OIDCIssuer: cfg.OIDCIssuer, OIDCClientID: cfg.OIDCClientID, IGDBClientID: cfg.IGDBClientID,
		IGDBClientIDFromEnvironment: b.IGDBClientIDFromEnvironment,
		OIDCSecretConfigured:        cfg.OIDCClientSecret != "",
		AppriseURL:                  cfg.AppriseURL, Development: cfg.Environment == "development", PrivateHosts: cfg.PrivateHosts(),
		LibraryURLs:       setupURLs,
		SecretsConfigured: map[secrets.Name]bool{}, LibraryUsernames: usernames,
	}
	for name, field := range secretFields(&cfg) {
		rt.Setup.SecretsConfigured[name] = *field != ""
	}
	return rt, nil
}

// providerStatuses is the provider half of operational status, derived from the
// resolved configuration; the live providerSet republishes it on every swap.
func providerStatuses(cfg config.Config, unreadable map[secrets.Name]string) []opstatus.Provider {
	return []opstatus.Provider{
		{Key: "tmdb", Name: "TMDB", Configured: cfg.TMDBReadToken != "", Note: unreadable[secrets.TMDBReadToken]},
		{Key: "musicbrainz", Name: "MusicBrainz", Configured: true},
		// IGDB needs both halves; a half-set pair is reported as not configured
		// instead of failing startup, because the client ID and the secret are
		// entered separately (D025).
		{Key: "igdb", Name: "IGDB", Configured: cfg.IGDBClientID != "" && cfg.IGDBClientSecret != "", Note: unreadable[secrets.IGDBClientSecret]},
		{Key: "apprise", Name: "Apprise", Configured: cfg.AppriseURL != "" && cfg.AppriseConfigKey != "", Note: unreadable[secrets.AppriseConfigKey]},
	}
}
