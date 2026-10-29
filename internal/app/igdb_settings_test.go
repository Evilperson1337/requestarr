package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/evilperson1337/requestarr/internal/config"
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
