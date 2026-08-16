package postgres

import (
	"context"
	"crypto/subtle"
	"errors"

	"github.com/jackc/pgx/v5"

	"git.example.com/evilperson1337/Requestarr/internal/secrets"
	"git.example.com/evilperson1337/Requestarr/internal/setup"
)

type SetupWorkflowRepository struct {
	pool    *Pool
	secrets *EncryptedSecretRepository
	cipher  *secrets.Cipher
}

func NewSetupWorkflowRepository(pool *Pool, secretRepository *EncryptedSecretRepository, cipher *secrets.Cipher) *SetupWorkflowRepository {
	return &SetupWorkflowRepository{pool: pool, secrets: secretRepository, cipher: cipher}
}

var setupAuthenticationSecretContext = secrets.Context{
	Namespace: "setup.authentication",
	OwnerID:   secrets.OwnerID{15: 1},
	Name:      "client_secret",
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
	result, err := decodeWorkflowState(current, draft)
	if err != nil {
		return setup.WorkflowResult{}, err
	}
	if r.secrets != nil {
		_, secretErr := r.secrets.Read(ctx, setupAuthenticationSecretContext)
		if secretErr == nil {
			result.State.ClientSecretConfigured = true
		} else if !errors.Is(secretErr, ErrEncryptedSecretNotFound) {
			return setup.WorkflowResult{}, setup.ErrPersistenceUnavailable
		}
	}
	return result, nil
}

func (r *SetupWorkflowRepository) SaveGeneral(ctx context.Context, digest setup.Digest, general setup.General) (result setup.WorkflowResult, err error) {
	if ctx.Err() != nil {
		return result, setup.ErrPersistenceCanceled
	}
	if r == nil || r.pool == nil || r.pool.pool == nil {
		return result, setup.ErrPersistenceUnavailable
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
	if existingState.State.Authentication != nil {
		present, presenceErr := r.secretPresentTx(ctx, tx)
		if presenceErr != nil {
			return result, presenceErr
		}
		existingState.State.ClientSecretConfigured = present
	}
	next := current
	if stepOrder(current) < stepOrder(setup.StepAuthentication) {
		next = setup.StepAuthentication
	}
	if current == next && existingState.State.General != nil && *existingState.State.General == general {
		if err = tx.Commit(ctx); err != nil {
			return result, classifySetupPersistenceError(ctx, err)
		}
		existingState.State.Current = next
		return existingState, nil
	}
	draft, err := setup.EncodeWorkflowDraft(setup.WorkflowDraft{General: &general, Authentication: existingState.State.Authentication})
	if err != nil {
		return result, setup.ErrInvalidPersistenceInput
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
	existingState.State.Current = next
	existingState.State.General = &general
	return existingState, nil
}

func (r *SetupWorkflowRepository) SaveAuthentication(ctx context.Context, digest setup.Digest, authentication setup.Authentication, operation setup.SecretOperation) (result setup.WorkflowResult, err error) {
	if ctx.Err() != nil {
		return result, setup.ErrPersistenceCanceled
	}
	if r == nil || r.pool == nil || r.pool.pool == nil || r.secrets == nil {
		return result, setup.ErrPersistenceUnavailable
	}
	tx, err := r.pool.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return result, classifySetupPersistenceError(ctx, err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var complete bool
	var current setup.Step
	var existing []byte
	if err = tx.QueryRow(ctx, `SELECT complete,current_step,draft FROM setup_state WHERE singleton=TRUE FOR UPDATE`).
		Scan(&complete, &current, &existing); err != nil {
		return result, classifySetupPersistenceError(ctx, err)
	}
	if complete {
		return setup.WorkflowResult{Outcome: setup.WorkflowCompleted}, commitWorkflow(ctx, tx)
	}
	var sessionValid bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM setup_sessions WHERE token_digest=$1
		AND revoked_at IS NULL AND expires_at > clock_timestamp())`, digest[:]).Scan(&sessionValid); err != nil {
		return result, classifySetupPersistenceError(ctx, err)
	}
	if !sessionValid {
		return setup.WorkflowResult{Outcome: setup.WorkflowSessionRequired}, commitWorkflow(ctx, tx)
	}
	state, err := decodeWorkflowState(current, existing)
	if err != nil {
		return result, err
	}
	envelope, readErr := r.secrets.ReadTx(ctx, tx, setupAuthenticationSecretContext)
	present := readErr == nil
	if readErr != nil && !errors.Is(readErr, ErrEncryptedSecretNotFound) {
		return result, setup.ErrPersistenceUnavailable
	}
	if authentication.ClientType == setup.ClientTypePublic && operation.Kind() != setup.SecretClear {
		return result, setup.ErrInvalidPersistenceInput
	}
	if authentication.ClientType == setup.ClientTypeConfidential && operation.Kind() == setup.SecretClear {
		return result, setup.ErrInvalidPersistenceInput
	}
	if authentication.ClientType == setup.ClientTypeConfidential && operation.Kind() == setup.SecretPreserve && !present {
		return result, setup.ErrInvalidPersistenceInput
	}
	secretChanged := false
	switch operation.Kind() {
	case setup.SecretPreserve:
	case setup.SecretClear:
		if present {
			if _, err = r.secrets.DeleteTx(ctx, tx, setupAuthenticationSecretContext); err != nil {
				return result, setup.ErrPersistenceUnavailable
			}
			secretChanged, present = true, false
		}
	case setup.SecretReplace:
		if r.cipher == nil {
			return setup.WorkflowResult{Outcome: setup.WorkflowLocked}, commitWorkflow(ctx, tx)
		}
		if present {
			plaintext, decryptErr := r.cipher.Decrypt(setupAuthenticationSecretContext, envelope)
			if decryptErr != nil {
				return setup.WorkflowResult{Outcome: setup.WorkflowLocked}, commitWorkflow(ctx, tx)
			}
			equal := false
			_ = operation.EncryptValue(func(candidate []byte) error {
				equal = len(candidate) == len(plaintext) && subtle.ConstantTimeCompare(candidate, plaintext) == 1
				return nil
			})
			for i := range plaintext {
				plaintext[i] = 0
			}
			if equal {
				break
			}
		}
		var encrypted secrets.Envelope
		if err = operation.EncryptValue(func(plaintext []byte) error {
			var encryptErr error
			encrypted, encryptErr = r.cipher.Encrypt(setupAuthenticationSecretContext, plaintext)
			return encryptErr
		}); err != nil {
			return setup.WorkflowResult{Outcome: setup.WorkflowLocked}, commitWorkflow(ctx, tx)
		}
		if err = r.secrets.UpsertTx(ctx, tx, setupAuthenticationSecretContext, encrypted); err != nil {
			if errors.Is(err, ErrEncryptionKeyRequired) || errors.Is(err, ErrEncryptionKeyIncompatible) {
				return setup.WorkflowResult{Outcome: setup.WorkflowLocked}, commitWorkflow(ctx, tx)
			}
			return result, setup.ErrPersistenceUnavailable
		}
		secretChanged, present = true, true
	default:
		return result, setup.ErrInvalidPersistenceInput
	}
	next := current
	if stepOrder(current) < stepOrder(setup.StepProviders) {
		next = setup.StepProviders
	}
	draftChanged := state.State.Authentication == nil || !setup.AuthenticationEqual(*state.State.Authentication, authentication) || next != current
	if !draftChanged && !secretChanged {
		state.State.ClientSecretConfigured = present
		return state, commitWorkflow(ctx, tx)
	}
	draft, err := setup.EncodeWorkflowDraft(setup.WorkflowDraft{General: state.State.General, Authentication: &authentication})
	if err != nil {
		return result, setup.ErrInvalidPersistenceInput
	}
	if _, err = tx.Exec(ctx, `UPDATE setup_state SET current_step=$1,draft=$2,updated_at=transaction_timestamp() WHERE singleton=TRUE`, next, draft); err != nil {
		return result, classifySetupPersistenceError(ctx, err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO setup_workflow_events(event_type,created_at) VALUES ('authentication_saved',clock_timestamp())`); err != nil {
		return result, classifySetupPersistenceError(ctx, err)
	}
	if err = tx.Commit(ctx); err != nil {
		return result, classifySetupPersistenceError(ctx, err)
	}
	return setup.WorkflowResult{Outcome: setup.WorkflowAvailable, State: setup.WorkflowState{Current: next, General: state.State.General, Authentication: &authentication, ClientSecretConfigured: present}}, nil
}

func decodeWorkflowState(current setup.Step, draft []byte) (setup.WorkflowResult, error) {
	if !current.Valid() {
		return setup.WorkflowResult{}, setup.ErrStateInvalid
	}
	decoded, err := setup.DecodeWorkflowDraft(draft)
	if err != nil {
		return setup.WorkflowResult{}, err
	}
	return setup.WorkflowResult{Outcome: setup.WorkflowAvailable, State: setup.WorkflowState{Current: current, General: decoded.General, Authentication: decoded.Authentication}}, nil
}

func (r *SetupWorkflowRepository) secretPresentTx(ctx context.Context, tx pgx.Tx) (bool, error) {
	if r.secrets == nil {
		return false, setup.ErrPersistenceUnavailable
	}
	_, err := r.secrets.ReadTx(ctx, tx, setupAuthenticationSecretContext)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, ErrEncryptedSecretNotFound) {
		return false, nil
	}
	return false, setup.ErrPersistenceUnavailable
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
