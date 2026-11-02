package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/evilperson1337/requestarr/internal/config"
	"github.com/evilperson1337/requestarr/internal/jobs"
	"github.com/evilperson1337/requestarr/internal/opstatus"
	"github.com/evilperson1337/requestarr/internal/secrets"
	"github.com/evilperson1337/requestarr/internal/sources"
)

// stopWait bounds how long a reconfiguration waits for the old schedule to wind down.
const stopWait = 5 * time.Second

type running struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// sourceManager owns the scheduled sync of every library Source and rebuilds one
// when its saved connection or credential changes (D028): no restart needed.
type sourceManager struct {
	ctx      context.Context
	workers  *sync.WaitGroup
	runner   *jobs.Runner
	logger   *slog.Logger
	envCfg   config.Config // the environment alone; resolution layers saved values over it
	store    bootStore
	adapters Adapters
	syncer   *sourceSyncer
	ops      *opstatus.Service
	secrets  *secrets.Service

	mu      sync.Mutex
	running map[string]*running
}

// Active reports whether a Source's sync is scheduled.
func (m *sourceManager) Active(slug string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.running[slug]
	return ok
}

// adopt starts the schedule of every Source Build configured at startup.
func (m *sourceManager) adopt(jobs []SourceJob) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, job := range jobs {
		m.start(job.Slug, job)
	}
}

func (m *sourceManager) start(slug string, source SourceJob) {
	ctx, cancel := context.WithCancel(m.ctx)
	done := make(chan struct{})
	m.running[slug] = &running{cancel: cancel, done: done}
	m.syncer.set(slug, syncSource{id: source.ID, job: source.Job})
	m.workers.Add(1)
	go func() {
		defer m.workers.Done()
		defer close(done)
		m.runner.Schedule(ctx, sourceSyncInterval, source.ID, source.Job)
	}()
}

func (m *sourceManager) stop(slug string) {
	r, ok := m.running[slug]
	if !ok {
		return
	}
	r.cancel()
	select {
	case <-r.done:
	case <-time.After(stopWait):
		m.logger.Warn("source schedule slow to stop", "source", slug)
	}
	delete(m.running, slug)
	m.syncer.remove(slug)
}

// ErrIncomplete means the Source's URL and credentials do not yet go together;
// the Source is off until they do. Missing says what is absent.
type ErrIncomplete struct{ Missing []string }

func (e ErrIncomplete) Error() string { return "the connection is incomplete" }

// Apply re-resolves one Source from the environment, the saved connection and
// the stored credentials, and starts, restarts or stops its sync to match.
func (m *sourceManager) Apply(ctx context.Context, slug string) error {
	kind, ok := sources.Lookup(slug)
	if !ok {
		return jobs.ErrUnknownSource
	}
	b, err := resolveConfig(ctx, m.envCfg, m.store, m.logger)
	if err != nil {
		return fmt.Errorf("resolve configuration: %w", err)
	}
	cfg := b.Config
	m.mu.Lock()
	defer m.mu.Unlock()
	defer m.refresh(b.Unreadable)
	if *cfg.LibraryURL(slug) == "" {
		m.stop(slug)
		return m.applied(ctx, kind)
	}
	if problems := cfg.LibraryProblems(slug); len(problems) > 0 {
		m.stop(slug)
		return ErrIncomplete{Missing: missingFields(cfg, kind)}
	}
	client, decorate, err := m.adapters.NewSource(cfg, kind)
	if err != nil {
		m.stop(slug)
		return fmt.Errorf("initialize %s integration: %w", kind.Name, err)
	}
	job := sourceSyncJob(m.adapters.Logger, m.adapters.SyncStore, client, *cfg.LibraryURL(slug))
	if decorate != nil {
		job = decorate(job)
	}
	m.stop(slug)
	m.start(slug, SourceJob{Slug: slug, ID: client.Descriptor().ID, Job: job})
	return m.applied(ctx, kind)
}

func (m *sourceManager) applied(ctx context.Context, kind sources.Kind) error {
	var names []secrets.Name
	for _, credential := range kind.Credentials {
		if credential.Secret != "" {
			names = append(names, secrets.Name(credential.Secret))
		}
	}
	if m.secrets == nil {
		return nil
	}
	if err := m.secrets.Applied(ctx, names...); err != nil && !errors.Is(err, context.Canceled) {
		m.logger.Warn("could not record applied secrets", "source", kind.Slug)
	}
	return nil
}

func (m *sourceManager) refresh(unreadable map[secrets.Name]string) {
	m.ops.SetSources(sourceRefs(m.syncer.snapshot(), unreadable))
}

// missingFields names what a half-configured Source still needs, by label.
func missingFields(cfg config.Config, kind sources.Kind) []string {
	var missing []string
	if *cfg.LibraryURL(kind.Slug) == "" {
		missing = append(missing, "server address")
	}
	for _, credential := range kind.Credentials {
		if *cfg.LibraryCredential(kind.Slug, credential.Role) == "" {
			missing = append(missing, credential.Noun)
		}
	}
	return missing
}
