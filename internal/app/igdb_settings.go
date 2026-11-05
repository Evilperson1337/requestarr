package app

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/evilperson1337/requestarr/internal/config"
	"github.com/evilperson1337/requestarr/internal/httpapi"
)

type igdbSettingsStore interface {
	LoadIGDBClientID(context.Context) (string, error)
}

// The client ID is a non-secret setting (an older installation may still have
// it in the retired setup draft; the store reads that as a fallback). Like
// stored secrets it applies on restart; an explicit environment value always wins.
func resolveIGDBClientID(ctx context.Context, cfg config.Config, repo igdbSettingsStore) (config.Config, error) {
	if cfg.IGDBClientID != "" {
		return cfg, nil
	}
	id, err := repo.LoadIGDBClientID(ctx)
	if err != nil {
		return cfg, errors.New("load saved IGDB client ID failed")
	}
	if !validIGDBClientID(id) {
		return cfg, errors.New("invalid saved IGDB client ID")
	}
	cfg.IGDBClientID = id
	return cfg, nil
}

func validIGDBClientID(id string) bool {
	return utf8.ValidString(id) && utf8.RuneCountInString(id) <= 256 && !strings.ContainsAny(id, "\x00\r\n")
}

type igdbClientIDStore interface {
	igdbSettingsStore
	SetIGDBClientID(context.Context, string) error
}

// igdbSettingsService is the admin-facing client ID setting. envID is the value
// the environment supplied at startup; when set it wins and cannot be changed.
type igdbSettingsService struct {
	store igdbClientIDStore
	envID string
}

func (s igdbSettingsService) IGDBClientID(ctx context.Context) (string, bool, error) {
	if s.envID != "" {
		return s.envID, true, nil
	}
	id, err := s.store.LoadIGDBClientID(ctx)
	return id, false, err
}

func (s igdbSettingsService) SetIGDBClientID(ctx context.Context, id string) error {
	if s.envID != "" {
		return httpapi.ErrIGDBClientIDFromEnvironment
	}
	if !validIGDBClientID(id) {
		return httpapi.ErrInvalidIGDBClientID
	}
	return s.store.SetIGDBClientID(ctx, id)
}
