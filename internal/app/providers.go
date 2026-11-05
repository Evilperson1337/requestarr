package app

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/evilperson1337/requestarr/internal/config"
	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/httpapi"
	"github.com/evilperson1337/requestarr/internal/notifications"
	"github.com/evilperson1337/requestarr/internal/notifications/apprise"
	"github.com/evilperson1337/requestarr/internal/opstatus"
	"github.com/evilperson1337/requestarr/internal/providers/metadata"
	"github.com/evilperson1337/requestarr/internal/providers/metadata/audible"
	"github.com/evilperson1337/requestarr/internal/providers/metadata/igdb"
	"github.com/evilperson1337/requestarr/internal/providers/metadata/musicbrainz"
	"github.com/evilperson1337/requestarr/internal/providers/metadata/tmdb"
	"github.com/evilperson1337/requestarr/internal/secrets"
)

// providerSnapshot is everything built from the TMDB token, the IGDB credentials
// and the Apprise key. It is immutable once published: a change builds a new one
// (D039), so a call that started on one snapshot finishes on it.
type providerSnapshot struct {
	resolver *metadata.Service
	search   *metadata.SearchService
	// notifier is nil while Apprise is off (no URL, or no key).
	notifier   *apprise.Client
	appriseKey string
	statuses   []opstatus.Provider
}

// providerSet holds the current providerSnapshot and swaps it atomically.
// Handlers and the notification worker read it per call (it implements the
// resolver, search and notification-sender seams); Reload rebuilds it from the
// resolved configuration after a credential is saved or cleared.
type providerSet struct {
	cur atomic.Pointer[providerSnapshot]

	// reloadMu serializes rebuilds, so the newest save is always the last one
	// resolved and an older one can never overwrite it.
	reloadMu sync.Mutex
	resolve  func(context.Context) (booted, error)
	catalog  metadata.Catalog
	onStatus func([]opstatus.Provider)

	// One instance each serves links, previews and searches for the process
	// lifetime: MusicBrainz spaces its requests per instance, and neither
	// takes a credential, so a reload has nothing to change in them.
	mb       *musicbrainz.Resolver
	audibler *audible.Resolver
}

// errReloadFailed is all a failed Reload says; the previous providers stay in use.
var errReloadFailed = errors.New("could not rebuild the providers from the saved configuration")

// errNotificationsOff is what a send returns while Apprise is not configured.
var errNotificationsOff = notifications.ErrNotConfigured

// newProviderSet builds the first snapshot from the startup configuration.
// resolve re-resolves the configuration on Reload (environment, then the saved
// client ID and stored secrets). onStatus, when set, receives the provider
// status list of every published snapshot.
func newProviderSet(initial config.Config, unreadable map[secrets.Name]string, catalog metadata.Catalog, resolve func(context.Context) (booted, error), onStatus func([]opstatus.Provider)) *providerSet {
	p := &providerSet{
		resolve: resolve, catalog: catalog, onStatus: onStatus,
		mb: musicbrainz.New(nil), audibler: audible.New(domain.AudibleRegion(initial.AudibleRegion), nil),
	}
	p.publish(p.build(initial, unreadable))
	return p
}

// build constructs a snapshot without touching the live one.
func (p *providerSet) build(cfg config.Config, unreadable map[secrets.Name]string) *providerSnapshot {
	tmdbProvider := tmdb.New(cfg.TMDBReadToken, nil)
	igdbProvider := igdb.New(cfg.IGDBClientID, cfg.IGDBClientSecret, nil)
	snap := &providerSnapshot{
		resolver:   metadata.NewService(p.catalog, tmdbProvider, p.mb, igdbProvider, p.audibler),
		appriseKey: cfg.AppriseConfigKey,
		statuses:   providerStatuses(cfg, unreadable),
	}
	// A search type exists only when its provider is configured; it is the one switch.
	searchers := map[string]metadata.Searcher{}
	if cfg.TMDBReadToken != "" {
		searchers["movie_tv"] = tmdbProvider
	}
	// MusicBrainz needs no key, so album and song search are always on.
	searchers["album"] = p.mb
	searchers["song"] = p.mb.SongSearcher()
	if cfg.IGDBClientID != "" && cfg.IGDBClientSecret != "" {
		searchers["game"] = igdbProvider
	}
	// An unset marketplace means US, so audiobook search is always on; the
	// catalog endpoint is undocumented, so failures reach the person as errors.
	searchers["audiobook"] = p.audibler
	snap.search = metadata.NewSearchService(searchers)
	if cfg.AppriseURL != "" && cfg.AppriseConfigKey != "" {
		snap.notifier = apprise.New(cfg.AppriseURL, cfg.AppriseConfigKey)
	}
	return snap
}

func (p *providerSet) publish(s *providerSnapshot) {
	p.cur.Store(s)
	if p.onStatus != nil {
		p.onStatus(s.statuses)
	}
}

// setOnStatus wires the status sink after construction and sends it the current list.
func (p *providerSet) setOnStatus(f func([]opstatus.Provider)) {
	p.reloadMu.Lock()
	defer p.reloadMu.Unlock()
	p.onStatus = f
	f(p.snapshot().statuses)
}

func (p *providerSet) snapshot() *providerSnapshot { return p.cur.Load() }

// Reload re-resolves the configuration and swaps in a new snapshot. When
// resolution fails the previous snapshot stays in place and the error returned
// carries no credential: resolution errors are fixed phrases.
func (p *providerSet) Reload(ctx context.Context) error {
	p.reloadMu.Lock()
	defer p.reloadMu.Unlock()
	b, err := p.resolve(ctx)
	if err != nil {
		// Not wrapped: whatever the resolver said may quote a saved value.
		return errReloadFailed
	}
	p.publish(p.build(b.Config, b.Unreadable))
	return nil
}

func (p *providerSet) ResolveAndUpsert(ctx context.Context, raw string) (domain.ResolvedItem, error) {
	return p.snapshot().resolver.ResolveAndUpsert(ctx, raw)
}

func (p *providerSet) Resolve(ctx context.Context, raw string) (domain.ResolvedItem, error) {
	return p.snapshot().resolver.Resolve(ctx, raw)
}

func (p *providerSet) Types() []string { return p.snapshot().search.Types() }

func (p *providerSet) Search(ctx context.Context, searchType, text string, page int, opts metadata.SearchOptions) (domain.SearchPage, error) {
	return p.snapshot().search.Search(ctx, searchType, text, page, opts)
}

// Configured reports whether notifications can be sent now; the worker leaves
// events in the outbox while it is false.
func (p *providerSet) Configured() bool { return p.snapshot().notifier != nil }

func (p *providerSet) Send(ctx context.Context, m notifications.Message) error {
	n := p.snapshot().notifier
	if n == nil {
		return errNotificationsOff
	}
	return n.Send(ctx, m)
}

// providerReloader is what the settings services call after a credential changes.
type providerReloader interface {
	Reload(context.Context) error
}

// providerSecrets are the stored secrets whose change rebuilds the providers.
var providerSecrets = map[secrets.Name]bool{
	secrets.TMDBReadToken:    true,
	secrets.IGDBClientSecret: true,
	secrets.AppriseConfigKey: true,
}

var _ httpapi.SearchService = (*providerSet)(nil)
var _ httpapi.Resolver = (*providerSet)(nil)
