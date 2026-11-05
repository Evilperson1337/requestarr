package app

import (
	"context"
	"log/slog"
	"time"
)

// firstAdminState is the public setup status: SetupComplete is true once an
// administrator exists, the same definition GET /setup/status reports.
type firstAdminState interface {
	SetupComplete(context.Context) (bool, error)
}

// announceFirstAdmin tells the operator, once, how to claim the first admin
// while none exists. The container has no shell on some hosts, so the log is
// where they look. The line carries no identity and never a token; the signed-in person's claim
// page shows their own command (D037).
// A failed lookup logs nothing at info level and never stops startup.
func announceFirstAdmin(ctx context.Context, logger *slog.Logger, state firstAdminState) {
	lookupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	complete, err := state.SetupComplete(lookupCtx)
	if err != nil {
		logger.Debug("first-admin status unavailable; not printing the claim command")
		return
	}
	if complete {
		return
	}
	logger.Info("no administrator exists yet. Sign in first (you become a viewer); the claim page shows the exact command to run, which looks like: docker exec <container-name> /requestarr setup-token --subject '<your OIDC user id>' --stdout",
		"token_valid_for", "30 minutes",
		"then", "paste the printed token on the claim page")
}
