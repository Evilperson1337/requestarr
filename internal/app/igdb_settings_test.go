package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/evilperson1337/requestarr/internal/config"
	"github.com/evilperson1337/requestarr/internal/httpapi"
)

type savedIGDBSettings struct {
	id    string
	err   error
	reads int
}

func (s *savedIGDBSettings) LoadIGDBClientID(context.Context) (string, error) {
	s.reads++
	return s.id, s.err
}

func TestIGDBStartupUsesSavedClientIDUnlessEnvironmentOverrides(t *testing.T) {
	for _, tc := range []struct {
		name, environment, stored, want string
		fail                            bool
	}{
		{name: "saved", stored: "saved-client", want: "saved-client"},
		{name: "environment wins", environment: "operator-client", stored: "saved-client", want: "operator-client"},
		{name: "fresh installation"},
		{name: "invalid saved ID", stored: "client\r\ninvalid", fail: true},
		{name: "oversized saved ID", stored: strings.Repeat("x", 257), fail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &savedIGDBSettings{id: tc.stored}
			cfg, err := resolveIGDBClientID(context.Background(), config.Config{IGDBClientID: tc.environment}, repo)
			if (err != nil) != tc.fail || !tc.fail && cfg.IGDBClientID != tc.want {
				t.Fatalf("client ID=%q error=%v", cfg.IGDBClientID, err)
			}
			if tc.environment != "" && repo.reads != 0 {
				t.Fatal("environment override unexpectedly read saved settings")
			}
		})
	}
	repo := &savedIGDBSettings{err: errors.New("database failure")}
	if _, err := resolveIGDBClientID(context.Background(), config.Config{}, repo); err == nil {
		t.Fatal("saved-settings read failure was swallowed")
	}
}

type memoryIGDBStore struct{ id string }

func (m *memoryIGDBStore) LoadIGDBClientID(context.Context) (string, error) { return m.id, nil }
func (m *memoryIGDBStore) SetIGDBClientID(_ context.Context, id string) error {
	m.id = id
	return nil
}

func TestIGDBSettingsServiceSavesUnlessTheEnvironmentOwnsTheValue(t *testing.T) {
	ctx := context.Background()
	store := &memoryIGDBStore{id: "saved"}
	svc := igdbSettingsService{store: store}
	if id, fromEnv, err := svc.IGDBClientID(ctx); err != nil || id != "saved" || fromEnv {
		t.Fatalf("id=%q fromEnv=%v err=%v", id, fromEnv, err)
	}
	if err := svc.SetIGDBClientID(ctx, "next"); err != nil || store.id != "next" {
		t.Fatalf("save: %v stored %q", err, store.id)
	}
	if err := svc.SetIGDBClientID(ctx, "bad\nid"); !errors.Is(err, httpapi.ErrInvalidIGDBClientID) {
		t.Fatalf("control characters accepted: %v", err)
	}
	if err := svc.SetIGDBClientID(ctx, strings.Repeat("x", 257)); !errors.Is(err, httpapi.ErrInvalidIGDBClientID) {
		t.Fatalf("oversized ID accepted: %v", err)
	}
	env := igdbSettingsService{store: store, envID: "operator"}
	if id, fromEnv, _ := env.IGDBClientID(ctx); id != "operator" || !fromEnv {
		t.Fatalf("environment must win: %q %v", id, fromEnv)
	}
	if err := env.SetIGDBClientID(ctx, "other"); !errors.Is(err, httpapi.ErrIGDBClientIDFromEnvironment) || store.id != "next" {
		t.Fatalf("environment-owned value was changed: %v stored %q", err, store.id)
	}
}
