package app

import (
	"context"
	"errors"

	"github.com/evilperson1337/requestarr/internal/config"
	"github.com/evilperson1337/requestarr/internal/sources"
)

type connectionStore interface {
	ListLibraryConnections(context.Context) (map[string]sources.Connection, error)
}

// connectionOrigin says where each Source's URL came from, for the Settings view.
type connectionOrigin struct {
	// FromEnvironment is true when the operator set the URL variable; a saved
	// connection never overrides it.
	FromEnvironment bool
	Saved           bool
}

// resolveLibraryConnections fills each Source's URL (and Navidrome's username)
// from the saved connection when the environment has none (D028). It runs
// before secrets so a stored credential can pair with a saved URL.
func resolveLibraryConnections(ctx context.Context, cfg config.Config, repo connectionStore) (config.Config, map[string]connectionOrigin, error) {
	saved, err := repo.ListLibraryConnections(ctx)
	if err != nil {
		return cfg, nil, errors.New("load saved library connections failed")
	}
	origins := map[string]connectionOrigin{}
	for _, kind := range sources.All() {
		url := cfg.LibraryURL(kind.Slug)
		conn, have := saved[kind.Slug]
		origin := connectionOrigin{FromEnvironment: *url != "", Saved: have}
		origins[kind.Slug] = origin
		if origin.FromEnvironment || !have || conn.Validate() != nil {
			continue
		}
		*url = conn.URL
		if _, takesUsername := kind.Credential(sources.RoleUsername); takesUsername && *cfg.LibraryCredential(kind.Slug, sources.RoleUsername) == "" {
			*cfg.LibraryCredential(kind.Slug, sources.RoleUsername) = conn.Username
		}
	}
	return cfg, origins, nil
}
