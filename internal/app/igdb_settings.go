package app

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/evilperson1337/requestarr/internal/config"
)

type igdbSettingsStore interface {
	LoadIGDBClientID(context.Context) (string, error)
}

// The client ID is non-secret and already saved in the setup draft. Like stored
// secrets it applies on restart; an explicit environment value always wins.
func resolveIGDBClientID(ctx context.Context, cfg config.Config, repo igdbSettingsStore) (config.Config, error) {
	if cfg.IGDBClientID != "" {
		return cfg, nil
	}
	id, err := repo.LoadIGDBClientID(ctx)
	if err != nil {
		return cfg, errors.New("load saved IGDB client ID failed")
	}
	if !utf8.ValidString(id) || utf8.RuneCountInString(id) > 256 || strings.ContainsAny(id, "\x00\r\n") {
		return cfg, errors.New("invalid saved IGDB client ID")
	}
	cfg.IGDBClientID = id
	return cfg, nil
}
