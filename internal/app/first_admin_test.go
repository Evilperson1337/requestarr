package app

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

type fakeSetupState struct {
	complete bool
	err      error
}

func (f fakeSetupState) SetupComplete(context.Context) (bool, error) { return f.complete, f.err }

func announce(t *testing.T, state fakeSetupState) string {
	t.Helper()
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	announceFirstAdmin(context.Background(), logger, state, "https://id.example.test/realms/home")
	return buf.String()
}

func TestAnnounceFirstAdminLogsTheClaimCommandOnceWhenNoAdminExists(t *testing.T) {
	out := announce(t, fakeSetupState{})
	if n := strings.Count(strings.TrimSpace(out), "\n") + 1; n != 1 {
		t.Fatalf("want exactly one log line, got %d: %q", n, out)
	}
	for _, want := range []string{
		"level=INFO",
		"setup-token --issuer 'https://id.example.test/realms/home' --subject '<OIDC user id>' --stdout",
		"30 minutes",
		"Setup page",
	} {
		if !strings.Contains(out, want) && !strings.Contains(out, strings.ReplaceAll(want, "'", `\'`)) {
			t.Errorf("log %q does not contain %q", out, want)
		}
	}
}

func TestAnnounceFirstAdminIsSilentOnceAnAdministratorExists(t *testing.T) {
	if out := announce(t, fakeSetupState{complete: true}); out != "" {
		t.Fatalf("logged %q", out)
	}
}

func TestAnnounceFirstAdminDoesNotStopStartupWhenTheLookupFails(t *testing.T) {
	if out := announce(t, fakeSetupState{err: errors.New("canary-secret-detail")}); out != "" {
		t.Fatalf("logged %q at info level", out)
	}
}
