package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/evilperson1337/requestarr/internal/setup"
	"github.com/jackc/pgx/v5"
)

var _ setup.Repository = (*Store)(nil)

// LoadIGDBClientID reads the non-secret ID without initializing or modifying the
// draft. No completed wizard is required: first-run checks need it before Finish.
func (s *Store) LoadIGDBClientID(ctx context.Context) (string, error) {
	var id string
	err := s.pool.QueryRow(ctx, `SELECT COALESCE(draft->'providers'->>'igdbClientId','') FROM setup_state WHERE singleton AND draft_revision > 0`).Scan(&id)
	if err == pgx.ErrNoRows {
		return "", nil
	}
	return id, err
}

// lockedSetupDraft serializes draft operations with bootstrap operations on the
// same singleton. A revision-zero row may still contain a legacy untyped draft.
func lockedSetupDraft(ctx context.Context, tx pgx.Tx) (setup.Draft, error) {
	d := setup.Draft{Validations: []setup.Validation{}, Inactive: true}
	var settings, validations []byte
	err := tx.QueryRow(ctx, `SELECT draft_revision,current_step,draft,draft_validations,updated_at,wizard_completed_at FROM setup_state WHERE singleton FOR UPDATE`).Scan(
		&d.Revision, &d.CurrentStep, &settings, &validations, &d.UpdatedAt, &d.CompletedAt)
	if err != nil {
		return setup.Draft{}, fmt.Errorf("lock setup draft: %w", err)
	}
	if d.Revision > 0 {
		if err := json.Unmarshal(settings, &d.Settings); err != nil {
			return setup.Draft{}, fmt.Errorf("decode setup settings: %w", err)
		}
		if err := json.Unmarshal(validations, &d.Validations); err != nil {
			return setup.Draft{}, fmt.Errorf("decode setup validations: %w", err)
		}
	}
	return d, nil
}

func auditSetupDraft(ctx context.Context, tx pgx.Tx, actor string, revision int64, eventType, target string) error {
	_, err := tx.Exec(ctx, `INSERT INTO setup_draft_events(actor_user_id,revision,event_type,target) VALUES($1,$2,$3,$4)`, actor, revision, eventType, target)
	if err != nil {
		return fmt.Errorf("audit setup draft: %w", err)
	}
	return nil
}

func (s *Store) LoadSetupDraft(ctx context.Context, initial setup.Draft, actor string) (setup.Draft, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return setup.Draft{}, err
	}
	defer tx.Rollback(ctx)
	d, err := lockedSetupDraft(ctx, tx)
	if err != nil {
		return setup.Draft{}, err
	}
	if d.Revision == 0 {
		settings, err := json.Marshal(initial.Settings)
		if err != nil {
			return setup.Draft{}, fmt.Errorf("encode setup settings: %w", err)
		}
		d.Settings, d.CurrentStep, d.Revision = initial.Settings, setup.StepGeneral, 1
		err = tx.QueryRow(ctx, `UPDATE setup_state SET draft=$1,current_step=$2,draft_revision=1,draft_validations='[]',updated_at=now() WHERE singleton RETURNING updated_at`, settings, d.CurrentStep).Scan(&d.UpdatedAt)
		if err != nil {
			return setup.Draft{}, fmt.Errorf("initialize setup draft: %w", err)
		}
		if err := auditSetupDraft(ctx, tx, actor, d.Revision, "initialized", string(d.CurrentStep)); err != nil {
			return setup.Draft{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return setup.Draft{}, err
	}
	return d, nil
}

func (s *Store) SaveSetupDraft(ctx context.Context, actor string, expected int64, step setup.Step, settings setup.Settings) (setup.Draft, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return setup.Draft{}, err
	}
	defer tx.Rollback(ctx)
	d, err := lockedSetupDraft(ctx, tx)
	if err != nil {
		return setup.Draft{}, err
	}
	if d.CompletedAt != nil {
		return setup.Draft{}, setup.ErrCompleted
	}
	if expected < 1 || d.Revision != expected {
		return setup.Draft{}, setup.ErrConflict
	}
	encoded, err := json.Marshal(settings)
	if err != nil {
		return setup.Draft{}, fmt.Errorf("encode setup settings: %w", err)
	}
	d.Settings, d.CurrentStep, d.Revision = settings, step, d.Revision+1
	d.Validations = []setup.Validation{}
	err = tx.QueryRow(ctx, `UPDATE setup_state SET draft=$1,current_step=$2,draft_revision=$3,draft_validations='[]',updated_at=now() WHERE singleton RETURNING updated_at`, encoded, step, d.Revision).Scan(&d.UpdatedAt)
	if err != nil {
		return setup.Draft{}, fmt.Errorf("save setup draft: %w", err)
	}
	if err := auditSetupDraft(ctx, tx, actor, d.Revision, "saved", string(step)); err != nil {
		return setup.Draft{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return setup.Draft{}, err
	}
	return d, nil
}

func (s *Store) RecordSetupValidation(ctx context.Context, actor string, expected int64, result setup.Validation) (setup.Draft, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return setup.Draft{}, err
	}
	defer tx.Rollback(ctx)
	d, err := lockedSetupDraft(ctx, tx)
	if err != nil {
		return setup.Draft{}, err
	}
	if d.CompletedAt != nil {
		return setup.Draft{}, setup.ErrCompleted
	}
	if expected < 1 || d.Revision != expected || result.Revision != expected {
		return setup.Draft{}, setup.ErrConflict
	}
	if !result.Target.Valid() {
		return setup.Draft{}, setup.ErrInvalid
	}
	replaced := false
	for i := range d.Validations {
		if d.Validations[i].Target == result.Target {
			d.Validations[i] = result
			replaced = true
			break
		}
	}
	if !replaced {
		d.Validations = append(d.Validations, result)
	}
	encoded, err := json.Marshal(d.Validations)
	if err != nil {
		return setup.Draft{}, fmt.Errorf("encode setup validations: %w", err)
	}
	err = tx.QueryRow(ctx, `UPDATE setup_state SET draft_validations=$1,updated_at=now() WHERE singleton RETURNING updated_at`, encoded).Scan(&d.UpdatedAt)
	if err != nil {
		return setup.Draft{}, fmt.Errorf("record setup validation: %w", err)
	}
	if err := auditSetupDraft(ctx, tx, actor, d.Revision, "validated", string(result.Target)); err != nil {
		return setup.Draft{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return setup.Draft{}, err
	}
	return d, nil
}

// CompleteSetupDraft marks the locked revision finished and audits it atomically.
func (s *Store) CompleteSetupDraft(ctx context.Context, actor string, expected int64) (setup.Draft, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return setup.Draft{}, err
	}
	defer tx.Rollback(ctx)
	d, err := lockedSetupDraft(ctx, tx)
	if err != nil {
		return setup.Draft{}, err
	}
	if d.CompletedAt != nil {
		return d, nil
	}
	if expected < 1 || d.Revision != expected {
		return setup.Draft{}, setup.ErrConflict
	}
	err = tx.QueryRow(ctx, `UPDATE setup_state SET wizard_completed_at=now(),wizard_completed_by=$1,updated_at=now() WHERE singleton RETURNING wizard_completed_at,updated_at`, actor).Scan(&d.CompletedAt, &d.UpdatedAt)
	if err != nil {
		return setup.Draft{}, fmt.Errorf("complete setup draft: %w", err)
	}
	if err := auditSetupDraft(ctx, tx, actor, d.Revision, "completed", string(setup.StepFinish)); err != nil {
		return setup.Draft{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return setup.Draft{}, err
	}
	return d, nil
}

// SetupWizardComplete reports whether an administrator finished the setup wizard.
func (s *Store) SetupWizardComplete(ctx context.Context) (bool, error) {
	var complete bool
	err := s.pool.QueryRow(ctx, `SELECT wizard_completed_at IS NOT NULL FROM setup_state WHERE singleton`).Scan(&complete)
	return complete, err
}
