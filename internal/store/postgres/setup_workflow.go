package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"

	"git.example.com/evilperson1337/Requestarr/internal/setup"
)

type SetupWorkflowRepository struct {
	pool *Pool
}

func NewSetupWorkflowRepository(pool *Pool) *SetupWorkflowRepository {
	return &SetupWorkflowRepository{pool: pool}
}

func (r *SetupWorkflowRepository) ReadWorkflow(ctx context.Context, digest setup.Digest) (setup.WorkflowResult, error) {
	if ctx.Err() != nil {
		return setup.WorkflowResult{}, setup.ErrPersistenceCanceled
	}
	if r == nil || r.pool == nil || r.pool.pool == nil {
		return setup.WorkflowResult{}, setup.ErrPersistenceUnavailable
	}
	var complete bool
	var current setup.Step
	var draft []byte
	var sessionValid bool
	err := r.pool.pool.QueryRow(ctx, `SELECT st.complete, st.current_step, st.draft,
		EXISTS (SELECT 1 FROM setup_sessions s WHERE s.token_digest = $1 AND s.revoked_at IS NULL
			AND s.expires_at > transaction_timestamp())
		FROM setup_state st WHERE st.singleton = TRUE`, digest[:]).Scan(&complete, &current, &draft, &sessionValid)
	if err != nil {
		return setup.WorkflowResult{}, classifySetupPersistenceError(ctx, err)
	}
	if !sessionValid {
		return setup.WorkflowResult{Outcome: setup.WorkflowSessionRequired}, nil
	}
	if complete {
		return setup.WorkflowResult{Outcome: setup.WorkflowCompleted}, nil
	}
	return decodeWorkflowState(current, draft)
}

func (r *SetupWorkflowRepository) SaveGeneral(ctx context.Context, digest setup.Digest, general setup.General) (result setup.WorkflowResult, err error) {
	if ctx.Err() != nil {
		return result, setup.ErrPersistenceCanceled
	}
	if r == nil || r.pool == nil || r.pool.pool == nil {
		return result, setup.ErrPersistenceUnavailable
	}
	draft, err := setup.EncodeWorkflowDraft(general)
	if err != nil {
		return result, setup.ErrInvalidPersistenceInput
	}
	tx, err := r.pool.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return result, classifySetupPersistenceError(ctx, err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	var complete bool
	var current setup.Step
	var existing []byte
	if err = tx.QueryRow(ctx, `SELECT complete, current_step, draft FROM setup_state
		WHERE singleton = TRUE FOR UPDATE`).Scan(&complete, &current, &existing); err != nil {
		return result, classifySetupPersistenceError(ctx, err)
	}
	// Mutation precedence is intentionally different from protected reads. Once
	// the serialized setup state is complete, report that terminal state before
	// consulting the session table, whose active rows completion revokes.
	if complete {
		return setup.WorkflowResult{Outcome: setup.WorkflowCompleted}, commitWorkflow(ctx, tx)
	}
	var sessionValid bool
	// clock_timestamp() is sampled by this statement after the setup-state lock
	// is acquired. transaction_timestamp() would preserve a stale pre-wait time.
	if err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM setup_sessions WHERE token_digest = $1
		AND revoked_at IS NULL AND expires_at > clock_timestamp())`, digest[:]).Scan(&sessionValid); err != nil {
		return result, classifySetupPersistenceError(ctx, err)
	}
	if !sessionValid {
		return setup.WorkflowResult{Outcome: setup.WorkflowSessionRequired}, commitWorkflow(ctx, tx)
	}
	existingState, err := decodeWorkflowState(current, existing)
	if err != nil {
		return result, err
	}
	next := current
	if stepOrder(current) < stepOrder(setup.StepAuthentication) {
		next = setup.StepAuthentication
	}
	if current == next && existingState.State.General != nil && *existingState.State.General == general {
		if err = tx.Commit(ctx); err != nil {
			return result, classifySetupPersistenceError(ctx, err)
		}
		return setup.WorkflowResult{Outcome: setup.WorkflowAvailable, State: setup.WorkflowState{Current: next, General: &general}}, nil
	}
	if _, err = tx.Exec(ctx, `UPDATE setup_state SET current_step = $1, draft = $2, updated_at = transaction_timestamp()
		WHERE singleton = TRUE`, next, draft); err != nil {
		return result, classifySetupPersistenceError(ctx, err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO setup_workflow_events (event_type, created_at)
		VALUES ('general_saved', clock_timestamp())`); err != nil {
		return result, classifySetupPersistenceError(ctx, err)
	}
	if err = tx.Commit(ctx); err != nil {
		return result, classifySetupPersistenceError(ctx, err)
	}
	return setup.WorkflowResult{Outcome: setup.WorkflowAvailable, State: setup.WorkflowState{Current: next, General: &general}}, nil
}

func decodeWorkflowState(current setup.Step, draft []byte) (setup.WorkflowResult, error) {
	if !current.Valid() {
		return setup.WorkflowResult{}, setup.ErrStateInvalid
	}
	general, err := setup.DecodeWorkflowDraft(draft)
	if err != nil {
		return setup.WorkflowResult{}, err
	}
	return setup.WorkflowResult{Outcome: setup.WorkflowAvailable, State: setup.WorkflowState{Current: current, General: general}}, nil
}

func commitWorkflow(ctx context.Context, tx pgx.Tx) error {
	if err := tx.Commit(ctx); err != nil {
		return classifySetupPersistenceError(ctx, err)
	}
	return nil
}

func stepOrder(step setup.Step) int {
	steps := []setup.Step{setup.StepWelcome, setup.StepGeneral, setup.StepAuthentication, setup.StepProviders,
		setup.StepLibraries, setup.StepNotifications, setup.StepPolicy, setup.StepReview, setup.StepFinish}
	for index, candidate := range steps {
		if candidate == step {
			return index
		}
	}
	return -1
}
