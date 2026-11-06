//go:build integration

package postgres

import (
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/auth"
)

func TestAuditRowsCarryACountAndPruningRemovesOnlyOldRows(t *testing.T) {
	ctx, store := freshStore(t)
	now := time.Now().UTC()
	old := now.Add(-91 * 24 * time.Hour)
	recent := now.Add(-89 * 24 * time.Hour)

	oldID, err := store.RecordAuthEventID(ctx, auth.AuditEvent{Type: auth.AuditSignInFailure, RemoteAddr: "198.51.100.1", At: old, Count: 1})
	if err != nil {
		t.Fatal("record failed")
	}
	recentID, err := store.RecordAuthEventID(ctx, auth.AuditEvent{Type: auth.AuditSignInThrottled, RemoteAddr: "198.51.100.1", At: recent})
	if err != nil {
		t.Fatal("record failed")
	}
	if err := store.AddAuthEventCount(ctx, recentID, 41); err != nil {
		t.Fatal("add count failed")
	}
	if got := count(t, ctx, store, `SELECT count FROM auth_audit_events WHERE id=$1`, recentID); got != 42 {
		t.Fatalf("count=%d, want 1 + 41", got)
	}
	// Old successes and password changes go with the retention too; recent ones stay.
	for _, e := range []auth.AuditEvent{
		{Type: auth.AuditSignInSuccess, At: old}, {Type: auth.AuditPasswordChanged, At: old},
		{Type: auth.AuditSignInSuccess, At: recent}, {Type: auth.AuditPasswordChanged, At: now},
	} {
		if err := store.RecordAuthEvent(ctx, e); err != nil {
			t.Fatal("record failed")
		}
	}
	n, err := store.PruneAuthEvents(ctx, now.Add(-90*24*time.Hour))
	if err != nil || n != 3 {
		t.Fatalf("pruned %d (%v), want the 3 rows older than 90 days", n, err)
	}
	if count(t, ctx, store, `SELECT count(*) FROM auth_audit_events WHERE id=$1`, oldID) != 0 {
		t.Fatal("an old row survived")
	}
	if got := count(t, ctx, store, `SELECT count(*) FROM auth_audit_events`); got != 3 {
		t.Fatalf("rows left=%d, want the 3 recent ones", got)
	}
	if got := count(t, ctx, store, `SELECT count(*) FROM auth_audit_events WHERE event_type IN ('signin_success','password_changed')`); got != 2 {
		t.Fatalf("recent success/password rows left=%d, want 2", got)
	}
}

func TestEveryOtherAuditPathStillWritesOneRowOfCountOne(t *testing.T) {
	ctx, store := freshStore(t)
	if err := store.RecordAuthEvent(ctx, auth.AuditEvent{Type: auth.AuditSignInFailure, At: time.Now()}); err != nil {
		t.Fatal("record failed")
	}
	if got := count(t, ctx, store, `SELECT count FROM auth_audit_events`); got != 1 {
		t.Fatalf("count=%d", got)
	}
}
