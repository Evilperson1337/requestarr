//go:build integration

package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/db/migrations"
	"github.com/evilperson1337/requestarr/internal/setup"
)

func setupDraftStore(t *testing.T) (context.Context, *Store, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	s, err := Open(ctx, testDatabaseURL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	// These tests run serially against the dedicated integration database.
	if _, err := s.pool.Exec(ctx, `UPDATE setup_state SET draft='{}',current_step='welcome',draft_revision=0,draft_validations='[]',wizard_completed_at=NULL,wizard_completed_by=NULL WHERE singleton`); err != nil {
		t.Fatal(err)
	}
	return ctx, s, integrationUser(t, ctx, s)
}

func initialSetupDraft() setup.Draft {
	return setup.Draft{Settings: setup.Settings{
		General:        setup.GeneralSettings{InstanceName: "Requestarr", Timezone: "UTC", PublicURL: "https://requestarr.test"},
		Authentication: setup.AuthenticationSettings{Issuer: "https://issuer.test", ClientID: "requestarr", ClientSecretEnv: setup.OIDCSecretEnv},
		Providers:      setup.ProviderSettings{TMDBEnabled: true, TMDBTokenEnv: setup.TMDBTokenEnv, IGDBClientSecretEnv: setup.IGDBSecretEnv},
	}, Environment: []setup.EnvironmentItem{{Name: "runtime-only", DesiredValue: "never-persist-this"}}}
}

func TestSetupDraftInitializesAndResumesIntegration(t *testing.T) {
	ctx, s, actor := setupDraftStore(t)
	initial := initialSetupDraft()
	initial.Revision, initial.CurrentStep = 99, setup.StepFinish
	draft, err := s.LoadSetupDraft(ctx, initial, actor)
	if err != nil {
		t.Fatal(err)
	}
	if draft.Revision != 1 || draft.CurrentStep != setup.StepGeneral || !reflect.DeepEqual(draft.Settings, initial.Settings) || draft.UpdatedAt.IsZero() || len(draft.Validations) != 0 || draft.Environment != nil {
		t.Fatalf("initialized draft = %+v", draft)
	}
	s.Close()
	reopened, err := Open(ctx, testDatabaseURL(t))
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	other := initialSetupDraft()
	other.Settings.General.InstanceName = "must not overwrite"
	resumed, err := reopened.LoadSetupDraft(ctx, other, actor)
	if err != nil || !reflect.DeepEqual(resumed, draft) {
		t.Fatalf("resumed = %+v, error = %v; want %+v", resumed, err, draft)
	}
}

func TestSetupDraftConcurrentSavesRejectStaleRevisionIntegration(t *testing.T) {
	ctx, s, actor := setupDraftStore(t)
	draft, err := s.LoadSetupDraft(ctx, initialSetupDraft(), actor)
	if err != nil {
		t.Fatal(err)
	}
	type outcome struct {
		draft setup.Draft
		err   error
	}
	start := make(chan struct{})
	results := make(chan outcome, 2)
	for _, name := range []string{"first writer", "second writer"} {
		go func() {
			<-start
			settings := draft.Settings
			settings.General.InstanceName = name
			d, err := s.SaveSetupDraft(ctx, actor, draft.Revision, setup.StepAuthentication, settings)
			results <- outcome{d, err}
		}()
	}
	close(start)
	var winner setup.Draft
	wins, conflicts := 0, 0
	for range 2 {
		r := <-results
		switch {
		case r.err == nil:
			wins++
			winner = r.draft
		case errors.Is(r.err, setup.ErrConflict):
			conflicts++
		default:
			t.Fatalf("save error = %v", r.err)
		}
	}
	if wins != 1 || conflicts != 1 || winner.Revision != 2 || winner.CurrentStep != setup.StepAuthentication {
		t.Fatalf("wins=%d conflicts=%d winner=%+v", wins, conflicts, winner)
	}
	if _, err := s.SaveSetupDraft(ctx, actor, 1, setup.StepProviders, draft.Settings); !errors.Is(err, setup.ErrConflict) {
		t.Fatalf("stale save error = %v", err)
	}
	resumed, err := s.LoadSetupDraft(ctx, initialSetupDraft(), actor)
	if err != nil || !reflect.DeepEqual(resumed, winner) {
		t.Fatalf("resumed = %+v error = %v, want %+v", resumed, err, winner)
	}
}

func TestSetupDraftValidationIsRevisionBoundIntegration(t *testing.T) {
	ctx, s, actor := setupDraftStore(t)
	draft, err := s.LoadSetupDraft(ctx, initialSetupDraft(), actor)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	failed := setup.Validation{Target: setup.TargetTMDB, Revision: draft.Revision, Code: setup.CheckUnavailable, Message: "Connection failed.", CheckedAt: now, ExpiresAt: now.Add(10 * time.Minute)}
	checked, err := s.RecordSetupValidation(ctx, actor, 1, failed)
	if err != nil || len(checked.Validations) != 1 || checked.Validations[0] != failed || checked.Revision != 1 || checked.CurrentStep != draft.CurrentStep || !reflect.DeepEqual(checked.Settings, draft.Settings) {
		t.Fatalf("failed validation = %+v, error = %v", checked, err)
	}
	resumed, err := s.LoadSetupDraft(ctx, initialSetupDraft(), actor)
	if err != nil || !reflect.DeepEqual(resumed, checked) {
		t.Fatalf("failed validation not retained: %+v, error = %v", resumed, err)
	}
	general := failed
	general.Target, general.Valid, general.Code = setup.TargetGeneral, true, setup.CheckOK
	if _, err := s.RecordSetupValidation(ctx, actor, 1, general); err != nil {
		t.Fatal(err)
	}
	passed := failed
	passed.Valid, passed.Code, passed.Message = true, setup.CheckOK, "Connected."
	checked, err = s.RecordSetupValidation(ctx, actor, 1, passed)
	if err != nil || len(checked.Validations) != 2 || checked.Validations[0] != passed || checked.Validations[1] != general {
		t.Fatalf("replacement validation = %+v, error = %v", checked, err)
	}
	// Even an unchanged-settings save creates a new revision and invalidates checks.
	saved, err := s.SaveSetupDraft(ctx, actor, 1, setup.StepProviders, draft.Settings)
	if err != nil || saved.Revision != 2 || len(saved.Validations) != 0 {
		t.Fatalf("saved = %+v, error = %v", saved, err)
	}
	for _, attempt := range []struct{ expected, resultRevision int64 }{{1, 1}, {2, 1}, {1, 2}} {
		stale := failed
		stale.Revision = attempt.resultRevision
		if _, err := s.RecordSetupValidation(ctx, actor, attempt.expected, stale); !errors.Is(err, setup.ErrConflict) {
			t.Fatalf("stale validation %+v error = %v", attempt, err)
		}
	}
	resumed, err = s.LoadSetupDraft(ctx, initialSetupDraft(), actor)
	if err != nil || !reflect.DeepEqual(resumed, saved) {
		t.Fatalf("stale validation changed draft: %+v, error = %v", resumed, err)
	}
}

func TestSetupDraftAuditFailureRollsBackIntegration(t *testing.T) {
	ctx, s, actor := setupDraftStore(t)
	missingActor := newUUID() // Valid UUID, but violates the audit actor foreign key.
	if _, err := s.LoadSetupDraft(ctx, initialSetupDraft(), missingActor); err == nil {
		t.Fatal("initialization without an auditable actor succeeded")
	}
	var revision int64
	if err := s.pool.QueryRow(ctx, `SELECT draft_revision FROM setup_state WHERE singleton`).Scan(&revision); err != nil || revision != 0 {
		t.Fatalf("initialization was not rolled back: revision=%d error=%v", revision, err)
	}
	draft, err := s.LoadSetupDraft(ctx, initialSetupDraft(), actor)
	if err != nil {
		t.Fatal(err)
	}
	settings := draft.Settings
	settings.General.InstanceName = "must roll back"
	if _, err := s.SaveSetupDraft(ctx, missingActor, 1, setup.StepProviders, settings); err == nil {
		t.Fatal("save without an auditable actor succeeded")
	}
	if _, err := s.RecordSetupValidation(ctx, missingActor, 1, setup.Validation{Target: setup.TargetTMDB, Revision: 1}); err == nil {
		t.Fatal("validation without an auditable actor succeeded")
	}
	resumed, err := s.LoadSetupDraft(ctx, initialSetupDraft(), actor)
	if err != nil || !reflect.DeepEqual(resumed, draft) {
		t.Fatalf("failed audit changed draft: %+v error=%v", resumed, err)
	}
	var count int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM setup_draft_events WHERE actor_user_id=$1`, actor).Scan(&count); err != nil || count != 1 {
		t.Fatalf("audit count=%d error=%v, want only initialization", count, err)
	}
}

type setupFailingChecker struct{}

func (setupFailingChecker) Check(context.Context, setup.Target, setup.Settings) (setup.CheckCode, error) {
	return setup.CheckUnavailable, errors.New("upstream credential=never-persist-this")
}

func TestSetupDraftStoresOnlyTypedConfigurationIntegration(t *testing.T) {
	ctx, s, actorID := setupDraftStore(t)
	actor := setup.Actor{ID: actorID, Role: "admin"}
	service := setup.NewService(s, setupFailingChecker{}, setup.Runtime{
		PublicURL: "https://requestarr.test", OIDCIssuer: "https://issuer.test", OIDCClientID: "requestarr",
		OIDCSecretConfigured: true, TMDBTokenConfigured: true, IGDBSecretConfigured: true,
	})
	draft, err := service.Load(ctx, actor)
	if err != nil {
		t.Fatal(err)
	}
	input := setup.SaveInput{ExpectedRevision: 1, CurrentStep: setup.StepAuthentication, Settings: draft.Settings}
	// Reject embedded URL credentials and arbitrary environment names before storage.
	for _, mutate := range []func(*setup.Settings){
		func(s *setup.Settings) { s.Authentication.Issuer = "https://user:never-persist-this@issuer.test" },
		func(s *setup.Settings) { s.Authentication.ClientSecretEnv = "never-persist-this" },
		func(s *setup.Settings) { s.Providers.TMDBTokenEnv = "never-persist-this" },
		func(s *setup.Settings) { s.Providers.IGDBClientSecretEnv = "never-persist-this" },
	} {
		invalid := input
		mutate(&invalid.Settings)
		if _, err := service.Save(ctx, actor, invalid); !errors.Is(err, setup.ErrInvalid) {
			t.Fatalf("unsafe settings error=%v", err)
		}
	}
	input.Settings.General.InstanceName = "Shared library"
	saved, err := service.Save(ctx, actor, input)
	if err != nil {
		t.Fatal(err)
	}
	checked, err := service.Validate(ctx, actor, saved.Revision, setup.TargetAuthentication)
	if err != nil || len(checked.Validations) != 1 || checked.Validations[0].Valid {
		t.Fatalf("checked=%+v error=%v", checked, err)
	}
	var raw, validations, audit string
	if err := s.pool.QueryRow(ctx, `SELECT draft::text,draft_validations::text FROM setup_state WHERE singleton`).Scan(&raw, &validations); err != nil {
		t.Fatal(err)
	}
	if err := s.pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(e) ORDER BY id)::text FROM setup_draft_events e WHERE actor_user_id=$1`, actorID).Scan(&audit); err != nil {
		t.Fatal(err)
	}
	var stored setup.Settings
	if err := json.Unmarshal([]byte(raw), &stored); err != nil || !reflect.DeepEqual(stored, input.Settings) {
		t.Fatalf("stored settings=%+v error=%v", stored, err)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &object); err != nil || len(object) != 6 || object["general"] == nil || object["authentication"] == nil || object["providers"] == nil || object["libraries"] == nil || object["notifications"] == nil || object["policy"] == nil {
		t.Fatalf("stored JSON is not the closed settings schema: %s error=%v", raw, err)
	}
	if strings.Contains(raw+validations+audit, "never-persist-this") {
		t.Fatal("secret marker was persisted")
	}
	for _, forbidden := range []string{"environment", "configured", "needsRestart", "inactive", "runtime"} {
		if strings.Contains(strings.ToLower(raw+validations+audit), `"`+strings.ToLower(forbidden)+`":`) {
			t.Fatalf("persisted runtime/secret field %q", forbidden)
		}
	}
	for _, env := range []string{setup.OIDCSecretEnv, setup.TMDBTokenEnv, setup.IGDBSecretEnv, setup.JellyfinAPIKeyEnv, setup.PlexTokenEnv, setup.RommTokenEnv, setup.NavidromeUsernameEnv, setup.NavidromePasswordEnv} {
		if !strings.Contains(raw, env) {
			t.Errorf("missing environment reference %q", env)
		}
	}
	var events []struct {
		Actor    string    `json:"actor_user_id"`
		Revision int64     `json:"revision"`
		Type     string    `json:"event_type"`
		Target   string    `json:"target"`
		Time     time.Time `json:"created_at"`
	}
	if err := json.Unmarshal([]byte(audit), &events); err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 {
		t.Fatalf("audit events=%s", audit)
	}
	for i, want := range []struct {
		kind, target string
		revision     int64
	}{{"initialized", "general", 1}, {"saved", "authentication", 2}, {"validated", "authentication", 2}} {
		e := events[i]
		if e.Actor != actorID || e.Type != want.kind || e.Target != want.target || e.Revision != want.revision || e.Time.IsZero() {
			t.Errorf("event=%+v want=%+v", e, want)
		}
	}
	if strings.Contains(audit, "Shared library") || strings.Contains(audit, "Connection check") {
		t.Fatal("audit includes settings or validation values")
	}
}

func TestSetupDraftPreservesBootstrapIntegration(t *testing.T) {
	ctx, s, actor := setupDraftStore(t)
	t.Cleanup(func() {
		_, err := s.pool.Exec(context.Background(), `UPDATE setup_state SET complete=false,target_issuer='',target_subject='',token_hash=NULL,token_expires_at=NULL,claimed_by=NULL,completed_at=NULL WHERE singleton`)
		if err != nil {
			t.Error(err)
		}
	})
	for _, complete := range []bool{false, true} {
		if _, err := s.pool.Exec(ctx, `UPDATE setup_state SET complete=$1,target_issuer='https://bootstrap.test',target_subject='bootstrap-subject',token_hash=decode('abcd','hex'),token_expires_at=now()+interval '5 minutes',claimed_by=$2,completed_at=now(),draft_revision=0 WHERE singleton`, complete, actor); err != nil {
			t.Fatal(err)
		}
		snapshot := func() string {
			t.Helper()
			var value string
			err := s.pool.QueryRow(ctx, `SELECT (to_jsonb(s)-'draft'-'draft_revision'-'draft_validations'-'current_step'-'updated_at')::text FROM setup_state s WHERE singleton`).Scan(&value)
			if err != nil {
				t.Fatal(err)
			}
			return value
		}
		before := snapshot()
		draft, err := s.LoadSetupDraft(ctx, initialSetupDraft(), actor)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.SaveSetupDraft(ctx, actor, draft.Revision, setup.StepLibraries, draft.Settings); err != nil {
			t.Fatal(err)
		}
		if _, err := s.RecordSetupValidation(ctx, actor, 2, setup.Validation{Target: setup.TargetGeneral, Revision: 2}); err != nil {
			t.Fatal(err)
		}
		if got := snapshot(); got != before {
			t.Fatalf("bootstrap changed: before=%s after=%s", before, got)
		}
		state, err := s.SetupComplete(ctx)
		if err != nil || state != complete {
			t.Fatalf("bootstrap complete=%v state=%+v error=%v", complete, state, err)
		}
	}
}

func TestSetupDraftMigrationRoundTripIntegration(t *testing.T) {
	ctx, s, actor := setupDraftStore(t)
	if _, err := s.LoadSetupDraft(ctx, initialSetupDraft(), actor); err != nil {
		t.Fatal(err)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx) // DDL is isolated and never changes migration history.
	var before string
	if err := tx.QueryRow(ctx, `SELECT (to_jsonb(s)-'draft_revision'-'draft_validations')::text FROM setup_state s WHERE singleton`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"000021_setup_drafts.down.sql", "000021_setup_drafts.up.sql"} {
		sql, err := migrations.FS.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, string(sql)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var after string
		if err := tx.QueryRow(ctx, `SELECT (to_jsonb(s)-'draft_revision'-'draft_validations')::text FROM setup_state s WHERE singleton`).Scan(&after); err != nil {
			t.Fatal(err)
		}
		if after != before {
			t.Fatalf("%s changed existing state", name)
		}
	}
	var revision int64
	var validations string
	if err := tx.QueryRow(ctx, `SELECT draft_revision,draft_validations::text FROM setup_state WHERE singleton`).Scan(&revision, &validations); err != nil || revision != 0 || validations != "[]" {
		t.Fatalf("migration defaults: revision=%d validations=%s error=%v", revision, validations, err)
	}
	for _, invalid := range []string{`UPDATE setup_state SET draft_revision=-1 WHERE singleton`, `UPDATE setup_state SET draft_validations='{}' WHERE singleton`} {
		if _, err := tx.Exec(ctx, `SAVEPOINT invalid_setup`); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, invalid); err == nil {
			t.Fatalf("invalid state accepted: %s", invalid)
		}
		if _, err := tx.Exec(ctx, `ROLLBACK TO SAVEPOINT invalid_setup`); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSetupDraftCompletionIsAuditedAndFreezesDraftIntegration(t *testing.T) {
	ctx, s, actor := setupDraftStore(t)
	draft, err := s.LoadSetupDraft(ctx, initialSetupDraft(), actor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CompleteSetupDraft(ctx, actor, draft.Revision+1); !errors.Is(err, setup.ErrConflict) {
		t.Fatalf("stale completion = %v", err)
	}
	if done, err := s.SetupWizardComplete(ctx); err != nil || done {
		t.Fatalf("complete before finish = %v, %v", done, err)
	}
	done, err := s.CompleteSetupDraft(ctx, actor, draft.Revision)
	if err != nil || done.CompletedAt == nil {
		t.Fatalf("complete = %+v, %v", done, err)
	}
	if again, err := s.CompleteSetupDraft(ctx, actor, draft.Revision); err != nil || again.CompletedAt == nil {
		t.Fatalf("repeat completion = %+v, %v", again, err)
	}
	if _, err := s.SaveSetupDraft(ctx, actor, draft.Revision, setup.StepGeneral, draft.Settings); !errors.Is(err, setup.ErrCompleted) {
		t.Fatalf("save after completion = %v", err)
	}
	if _, err := s.RecordSetupValidation(ctx, actor, draft.Revision, setup.Validation{Target: setup.TargetGeneral, Revision: draft.Revision}); !errors.Is(err, setup.ErrCompleted) {
		t.Fatalf("validation after completion = %v", err)
	}
	var events int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM setup_draft_events WHERE event_type='completed'`).Scan(&events); err != nil || events < 1 {
		t.Fatalf("completion audit events = %d, %v", events, err)
	}
	if finished, err := s.SetupWizardComplete(ctx); err != nil || !finished {
		t.Fatalf("complete after finish = %v, %v", finished, err)
	}
}
