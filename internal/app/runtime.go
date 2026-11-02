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
	// as opposed to it being loaded from the saved setup draft.
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
	if err := b.Config.Validate(); err != nil {
		return booted{}, fmt.Errorf("invalid configuration: %w", err)
	}
	return b, nil
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
	rt.Providers = []opstatus.Provider{
		{Key: "tmdb", Name: "TMDB", Configured: cfg.TMDBReadToken != "", Note: b.Unreadable[secrets.TMDBReadToken]},
		{Key: "musicbrainz", Name: "MusicBrainz", Configured: true},
		// IGDB needs both halves; a half-set pair is reported as not configured
		// instead of failing startup, because the saved client ID and the secret
		// are entered separately in Setup (D025).
		{Key: "igdb", Name: "IGDB", Configured: cfg.IGDBClientID != "" && cfg.IGDBClientSecret != "", Note: b.Unreadable[secrets.IGDBClientSecret]},
		{Key: "apprise", Name: "Apprise", Configured: cfg.AppriseURL != "", Note: b.Unreadable[secrets.AppriseConfigKey]},
	}
	rt.Setup = setup.Runtime{
		PublicURL: cfg.PublicURL, OIDCIssuer: cfg.OIDCIssuer, OIDCClientID: cfg.OIDCClientID, IGDBClientID: cfg.IGDBClientID,
		IGDBClientIDFromEnvironment: b.IGDBClientIDFromEnvironment,
		OIDCSecretConfigured:        cfg.OIDCClientSecret != "",
		AppriseURL:                  cfg.AppriseURL, Development: cfg.Environment == "development",
		LibraryURLs:       setupURLs,
		SecretsConfigured: map[secrets.Name]bool{}, LibraryUsernames: usernames,
	}
	for name, field := range secretFields(&cfg) {
		rt.Setup.SecretsConfigured[name] = *field != ""
	}
	return rt, nil
}
