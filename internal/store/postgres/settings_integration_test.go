//go:build integration

package postgres

import (
	"context"
	"testing"
	"time"
)

func settingsStore(t *testing.T) (context.Context, *Store) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	s, err := Open(ctx, testDatabaseURL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	reset := func() {
		// Serial tests against the dedicated integration database.
		_, _ = s.pool.Exec(context.Background(), `DELETE FROM app_settings WHERE key IN ('igdb.client_id','setup.checklist_dismissed','people.default_role','approval.auto_types')`)
		_, _ = s.pool.Exec(context.Background(), `UPDATE setup_state SET draft='{}',current_step='welcome',draft_revision=0,draft_validations='[]' WHERE singleton`)
	}
	reset()
	t.Cleanup(reset)
	return ctx, s
}

// seedLegacyDraft writes what the retired setup wizard saved for the client ID.
func seedLegacyDraft(t *testing.T, ctx context.Context, s *Store, id string) {
	t.Helper()
	if _, err := s.pool.Exec(ctx, `UPDATE setup_state SET draft=jsonb_build_object('providers', jsonb_build_object('igdbClientId',$1::text)),draft_revision=3 WHERE singleton`, id); err != nil {
		t.Fatal(err)
	}
}

func TestIGDBClientIDIsStoredAsASettingIntegration(t *testing.T) {
	ctx, s := settingsStore(t)
	if id, err := s.LoadIGDBClientID(ctx); err != nil || id != "" {
		t.Fatalf("fresh client ID=%q error=%v", id, err)
	}
	if err := s.SetIGDBClientID(ctx, "live-client"); err != nil {
		t.Fatal(err)
	}
	if id, err := s.LoadIGDBClientID(ctx); err != nil || id != "live-client" {
		t.Fatalf("client ID=%q error=%v", id, err)
	}
}

func TestIGDBClientIDFromTheRetiredDraftKeepsWorkingIntegration(t *testing.T) {
	ctx, s := settingsStore(t)
	seedLegacyDraft(t, ctx, s, "saved-before-upgrade")
	if id, err := s.LoadIGDBClientID(ctx); err != nil || id != "saved-before-upgrade" {
		t.Fatalf("legacy client ID=%q error=%v", id, err)
	}
	// Reading never rewrites the old draft or creates a setting.
	if _, ok, _ := s.Setting(ctx, "igdb.client_id"); ok {
		t.Fatal("the read-time fallback must not write a setting")
	}
}

func TestSavedIGDBClientIDBeatsTheLegacyDraftAndClearingStaysClearedIntegration(t *testing.T) {
	ctx, s := settingsStore(t)
	seedLegacyDraft(t, ctx, s, "old")
	if err := s.SetIGDBClientID(ctx, "new"); err != nil {
		t.Fatal(err)
	}
	if id, _ := s.LoadIGDBClientID(ctx); id != "new" {
		t.Fatalf("client ID=%q, want the setting to win", id)
	}
	if err := s.SetIGDBClientID(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if id, _ := s.LoadIGDBClientID(ctx); id != "" {
		t.Fatalf("client ID=%q, an explicit clear must not fall back to the draft", id)
	}
}

func TestChecklistDismissalAndPolicyReviewedIntegration(t *testing.T) {
	ctx, s := settingsStore(t)
	if d, err := s.ChecklistDismissed(ctx); err != nil || d {
		t.Fatalf("fresh dismissed=%v error=%v", d, err)
	}
	if set, err := s.PolicyReviewed(ctx); err != nil || set {
		t.Fatalf("fresh role set=%v error=%v", set, err)
	}
	if err := s.DismissChecklist(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.DismissChecklist(ctx); err != nil {
		t.Fatalf("dismissing twice: %v", err)
	}
	if d, _ := s.ChecklistDismissed(ctx); !d {
		t.Fatal("dismissal was not remembered")
	}
	if err := s.SetDefaultFirstSignInRole(ctx, "viewer"); err != nil {
		t.Fatal(err)
	}
	if set, _ := s.PolicyReviewed(ctx); !set {
		t.Fatal("saving the default role marks the policy reviewed")
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM app_settings WHERE key='people.default_role'`); err != nil {
		t.Fatal(err)
	}
	if err := s.SetAutoApproveTypes(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if set, _ := s.PolicyReviewed(ctx); !set {
		t.Fatal("saving the approval policy marks it reviewed too")
	}
}

func TestRetiredWizardColumnsRemainReadableIntegration(t *testing.T) {
	ctx, s := settingsStore(t)
	// The tables and columns of the retired wizard are left in place (D038).
	var n int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM setup_draft_events`).Scan(&n); err != nil {
		t.Fatalf("setup_draft_events: %v", err)
	}
}
