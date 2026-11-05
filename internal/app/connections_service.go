package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/evilperson1337/requestarr/internal/config"
	"github.com/evilperson1337/requestarr/internal/httpapi"
	"github.com/evilperson1337/requestarr/internal/jobs"
	"github.com/evilperson1337/requestarr/internal/secrets"
	"github.com/evilperson1337/requestarr/internal/setup"
	"github.com/evilperson1337/requestarr/internal/sources"
)

type connectionWriter interface {
	connectionStore
	// SaveLibraryConnection writes the connection (when not nil) and the sealed
	// credentials in one transaction.
	SaveLibraryConnection(ctx context.Context, actorID string, c *sources.Connection, sealed map[secrets.Name]secrets.Sealed) error
	DeleteLibraryConnection(context.Context, string) error
}

// connectionChecker is the connection probe, used for an explicit test.
type connectionChecker interface {
	Check(context.Context, setup.Target, setup.Settings) (setup.CheckCode, error)
	Describe(context.Context, setup.Target, setup.CheckCode) string
}

// connectionsService is where an administrator manages library Sources after
// setup (D028). Every change is applied at once through the sourceManager.
type connectionsService struct {
	store   connectionWriter
	boot    bootStore
	envCfg  config.Config
	manager *sourceManager
	secrets *secrets.Service
	checker connectionChecker
	logger  *slog.Logger
}

var _ httpapi.ConnectionsService = (*connectionsService)(nil)

func (s *connectionsService) view(ctx context.Context, slug string) (httpapi.Connection, error) {
	kind, ok := sources.Lookup(slug)
	if !ok {
		return httpapi.Connection{}, httpapi.ErrUnknownSource
	}
	b, err := resolveConfig(ctx, s.envCfg, s.boot, s.logger)
	if err != nil {
		return httpapi.Connection{}, err
	}
	statuses, _, err := s.secrets.Status(ctx)
	if err != nil {
		return httpapi.Connection{}, err
	}
	byName := map[string]secrets.Status{}
	for _, st := range statuses {
		byName[string(st.Name)] = st
	}
	cfg := b.Config
	_, takesUsername := kind.Credential(sources.RoleUsername)
	v := httpapi.Connection{
		Slug: kind.Slug, Name: kind.Name, URL: *cfg.LibraryURL(slug), URLEnv: kind.URLEnv,
		URLFromEnvironment: b.Connections[slug].FromEnvironment, Saved: b.Connections[slug].Saved,
		TakesUsername: takesUsername, Active: s.manager.Active(slug), Missing: []string{}, Credentials: []httpapi.ConnectionCredential{},
	}
	if takesUsername {
		v.Username = *cfg.LibraryCredential(slug, sources.RoleUsername)
	}
	if v.URL != "" && len(cfg.LibraryProblems(slug)) > 0 {
		v.Missing = missingFields(cfg, kind)
	}
	for _, credential := range kind.Credentials {
		if credential.Secret == "" {
			continue
		}
		st := byName[credential.Secret]
		v.Credentials = append(v.Credentials, httpapi.ConnectionCredential{Secret: credential.Secret, Label: st.Label, Configured: st.Configured, Source: string(st.Source), Unreadable: st.Unreadable})
	}
	return v, nil
}

func (s *connectionsService) ListConnections(ctx context.Context) ([]httpapi.Connection, error) {
	out := []httpapi.Connection{}
	for _, kind := range sources.All() {
		v, err := s.view(ctx, kind.Slug)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// apply rebuilds the Source. An incomplete connection is a normal state, not a failure.
func (s *connectionsService) apply(ctx context.Context, slug string) error {
	err := s.manager.Apply(ctx, slug)
	var incomplete ErrIncomplete
	if err == nil || errors.As(err, &incomplete) {
		return nil
	}
	if errors.Is(err, jobs.ErrUnknownSource) {
		return httpapi.ErrUnknownSource
	}
	return err
}

func (s *connectionsService) SaveConnection(ctx context.Context, actorID string, c sources.Connection, credentials map[secrets.Name]string) (httpapi.Connection, error) {
	kind, ok := sources.Lookup(c.Slug)
	if !ok {
		return httpapi.Connection{}, httpapi.ErrUnknownSource
	}
	// An empty address means "leave the address alone", which is how a credential
	// is saved for an address that an environment variable owns.
	changesAddress := c.URL != "" || len(credentials) == 0
	if changesAddress {
		if s.envCfg.LibraryURL(c.Slug) != nil && *s.envCfg.LibraryURL(c.Slug) != "" {
			return httpapi.Connection{}, httpapi.ErrSetByEnvironment
		}
		if err := c.Validate(); err != nil {
			return httpapi.Connection{}, err
		}
	}
	sealed := make(map[secrets.Name]secrets.Sealed, len(credentials))
	for name, value := range credentials {
		if !kindHasSecret(kind, name) {
			return httpapi.Connection{}, httpapi.ErrUnknownCredential
		}
		box, err := s.secrets.Seal(name, value)
		if err != nil {
			return httpapi.Connection{}, err
		}
		sealed[name] = box
	}
	var address *sources.Connection
	if changesAddress {
		address = &c
	}
	if err := s.store.SaveLibraryConnection(ctx, actorID, address, sealed); err != nil {
		return httpapi.Connection{}, fmt.Errorf("save library connection: %w", err)
	}
	if err := s.apply(ctx, c.Slug); err != nil {
		return httpapi.Connection{}, err
	}
	return s.view(ctx, c.Slug)
}

func kindHasSecret(kind sources.Kind, name secrets.Name) bool {
	for _, credential := range kind.Credentials {
		if credential.Secret != "" && credential.Secret == string(name) {
			return true
		}
	}
	return false
}

func (s *connectionsService) RemoveConnection(ctx context.Context, slug string) (httpapi.Connection, error) {
	if _, ok := sources.Lookup(slug); !ok {
		return httpapi.Connection{}, httpapi.ErrUnknownSource
	}
	if err := s.store.DeleteLibraryConnection(ctx, slug); err != nil {
		return httpapi.Connection{}, fmt.Errorf("remove library connection: %w", err)
	}
	if err := s.apply(ctx, slug); err != nil {
		return httpapi.Connection{}, err
	}
	return s.view(ctx, slug)
}

func (s *connectionsService) TestConnection(ctx context.Context, slug string) (httpapi.ConnectionTest, error) {
	kind, ok := sources.Lookup(slug)
	if !ok {
		return httpapi.ConnectionTest{}, httpapi.ErrUnknownSource
	}
	b, err := resolveConfig(ctx, s.envCfg, s.boot, s.logger)
	if err != nil {
		return httpapi.ConnectionTest{}, err
	}
	cfg := b.Config
	source := setup.SourceSettings{Enabled: true, URL: *cfg.LibraryURL(slug), MediaTypes: kind.DefaultMediaTypes()}
	if token, ok := kind.Credential(sources.RoleToken); ok {
		source.TokenEnv = token.Env
	}
	if password, ok := kind.Credential(sources.RolePassword); ok {
		source.PasswordEnv = password.Env
	}
	if _, ok := kind.Credential(sources.RoleUsername); ok {
		source.Username = *cfg.LibraryCredential(slug, sources.RoleUsername)
	}
	var settings setup.Settings
	*settings.Libraries.Source(slug) = source
	target := setup.LibraryTarget(slug)
	code, _ := s.checker.Check(ctx, target, settings)
	if code == setup.CheckOK {
		// The checker's generic success text talks about draft settings, which is not what this is.
		return httpapi.ConnectionTest{OK: true, Code: string(code), Message: kind.Name + " answered with the saved address and credentials."}, nil
	}
	return httpapi.ConnectionTest{OK: false, Code: string(code), Message: s.checker.Describe(ctx, target, code)}, nil
}

// reloadingSecrets applies a new credential at once instead of waiting for a
// restart: a library Source's through the sourceManager (D028), and the TMDB
// token, IGDB secret and Apprise key through the providerSet (D039). The value
// is written first, as SaveConnection does; if the rebuild then fails the
// previous running configuration stays and the save is reported as not applied.
type reloadingSecrets struct {
	*secrets.Service
	manager   *sourceManager
	providers providerReloader
	logger    *slog.Logger
}

func (r reloadingSecrets) reload(ctx context.Context, name secrets.Name) error {
	if providerSecrets[name] && r.providers != nil {
		if err := r.providers.Reload(ctx); err != nil {
			r.logger.Warn("could not apply the saved credential", "secret", string(name))
			return httpapi.ErrNotApplied
		}
		if err := r.Service.Applied(ctx, name); err != nil && !errors.Is(err, context.Canceled) {
			r.logger.Warn("could not record applied secrets", "secret", string(name))
		}
		return nil
	}
	kind, ok := unreadableSource(name)
	if !ok {
		return nil
	}
	var incomplete ErrIncomplete
	if err := r.manager.Apply(ctx, kind.Slug); err != nil && !errors.As(err, &incomplete) {
		r.logger.Warn("could not apply the saved credential", "source", kind.Slug)
	}
	return nil
}

func (r reloadingSecrets) Set(ctx context.Context, actorID string, name secrets.Name, value string) error {
	if err := r.Service.Set(ctx, actorID, name, value); err != nil {
		return err
	}
	return r.reload(ctx, name)
}

func (r reloadingSecrets) Clear(ctx context.Context, actorID string, name secrets.Name) error {
	if err := r.Service.Clear(ctx, actorID, name); err != nil {
		return err
	}
	return r.reload(ctx, name)
}
