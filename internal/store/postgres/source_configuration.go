package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"git.example.com/evilperson1337/Requestarr/internal/inventory"
	"git.example.com/evilperson1337/Requestarr/internal/secrets"
)

// SourceConfigurationRepository owns the source-configuration transaction. It
// intentionally has no dependency on a source reader or a job executor.
type SourceConfigurationRepository struct {
	pool    *Pool
	secrets *EncryptedSecretRepository
}

func NewSourceConfigurationRepository(pool *Pool, secretRepository *EncryptedSecretRepository) *SourceConfigurationRepository {
	return &SourceConfigurationRepository{pool: pool, secrets: secretRepository}
}

func (r *SourceConfigurationRepository) SaveSource(ctx context.Context, save inventory.SourceSave) (inventory.SourceSaveResult, error) {
	return r.SaveSourceConfiguration(ctx, inventory.SourceConfigurationSave{
		Configuration: save.Configuration, EndpointURL: save.EndpointURL,
		Credential:       inventory.SourceCredentialChange{Replace: save.Credential},
		ExpectedRevision: save.ExpectedRevision, ActorID: save.ActorID, AuditID: save.AuditID, OccurredAt: save.OccurredAt,
	})
}

const sourceConfigurationViewQuery = `SELECT s.id,s.name,s.kind,s.enabled,s.authoritative_media_types,c.endpoint_url,c.revision,
	COALESCE(array_agg(l.remote_library_id ORDER BY l.remote_library_id) FILTER (WHERE l.remote_library_id IS NOT NULL),'{}'),
	EXISTS(SELECT 1 FROM encrypted_secrets e WHERE e.namespace='inventory-source' AND e.owner_id=s.id AND e.secret_name='credential'),
	COALESCE(st.state,'idle'),st.last_started_at,st.last_completed_at,st.last_successful_at,COALESCE(st.failure_detail,'')
	FROM library_sources s
	JOIN inventory_source_configurations c ON c.source_id=s.id
	LEFT JOIN inventory_source_selected_libraries l ON l.source_id=s.id
	LEFT JOIN inventory_source_status st ON st.source_id=s.id
	WHERE s.kind IN ('plex','audiobookshelf')`

// ListSourceConfigurations returns local projections only. It performs no
// source, credential, adapter, or network reads.
func (r *SourceConfigurationRepository) ListSourceConfigurations(ctx context.Context) ([]inventory.SourceConfigurationView, error) {
	if err := r.available(ctx); err != nil {
		return nil, err
	}
	rows, err := r.pool.pool.Query(ctx, sourceConfigurationViewQuery+` GROUP BY s.id,c.endpoint_url,c.revision,st.state,st.last_started_at,st.last_completed_at,st.last_successful_at,st.failure_detail ORDER BY s.name,s.id`)
	if err != nil {
		return nil, classifyInventoryError(ctx, err)
	}
	defer rows.Close()
	views := make([]inventory.SourceConfigurationView, 0)
	for rows.Next() {
		view, err := scanSourceConfigurationView(rows)
		if err != nil {
			return nil, classifyInventoryError(ctx, err)
		}
		views = append(views, view)
	}
	if err := rows.Err(); err != nil {
		return nil, classifyInventoryError(ctx, err)
	}
	return views, nil
}

func (r *SourceConfigurationRepository) GetSourceConfiguration(ctx context.Context, sourceID string) (inventory.SourceConfigurationView, error) {
	if err := r.available(ctx); err != nil {
		return inventory.SourceConfigurationView{}, err
	}
	if !inventorySourceIDValid(sourceID) {
		return inventory.SourceConfigurationView{}, &inventory.SourceNotFoundError{SourceID: sourceID}
	}
	view, err := scanSourceConfigurationView(r.pool.pool.QueryRow(ctx, sourceConfigurationViewQuery+` AND s.id=$1 GROUP BY s.id,c.endpoint_url,c.revision,st.state,st.last_started_at,st.last_completed_at,st.last_successful_at,st.failure_detail`, sourceID))
	if err == pgx.ErrNoRows {
		return inventory.SourceConfigurationView{}, &inventory.SourceNotFoundError{SourceID: sourceID}
	}
	if err != nil {
		return inventory.SourceConfigurationView{}, classifyInventoryError(ctx, err)
	}
	return view, nil
}

type sourceConfigurationRow interface{ Scan(...any) error }

func scanSourceConfigurationView(row sourceConfigurationRow) (inventory.SourceConfigurationView, error) {
	var view inventory.SourceConfigurationView
	var mediaTypesJSON []byte
	var selectedLibraries []string
	var sourceKind, state string
	var started, completed, successful *time.Time
	err := row.Scan(&view.Configuration.ID, &view.Configuration.Name, &sourceKind, &view.Configuration.Enabled, &mediaTypesJSON, &view.EndpointURL, &view.Revision, &selectedLibraries, &view.CredentialConfigured, &state, &started, &completed, &successful, &view.Status.FailureDetail)
	if err != nil {
		return inventory.SourceConfigurationView{}, err
	}
	if err := json.Unmarshal(mediaTypesJSON, &view.Configuration.MediaTypes); err != nil {
		return inventory.SourceConfigurationView{}, err
	}
	view.Configuration.Kind = inventory.SourceKind(sourceKind)
	view.Configuration.SelectedLibraries = selectedLibraries
	view.Configuration.Capabilities = inventory.SourceCapabilities{CompleteEnumeration: true}
	view.Status.SourceID = view.Configuration.ID
	view.Status.State = inventory.SourceSyncState(state)
	if started != nil {
		view.Status.LastStartedAt = started.UTC()
	}
	if completed != nil {
		view.Status.LastCompletedAt = completed.UTC()
	}
	if successful != nil {
		view.Status.LastSuccessfulAt = successful.UTC()
	}
	return view, nil
}

func inventorySourceIDValid(id string) bool {
	// Keep malformed source identifiers on the same absence path without making
	// the unexported inventory durable-ID rule part of this persistence package.
	_, err := sourceSecretOwner(id)
	return err == nil
}

// SaveSourceConfiguration atomically commits configuration evidence and either
// preserves the existing encrypted credential or replaces it. It never reads a
// credential, which keeps this adapter's response surface secret-free.
func (r *SourceConfigurationRepository) SaveSourceConfiguration(ctx context.Context, save inventory.SourceConfigurationSave) (inventory.SourceSaveResult, error) {
	if err := inventory.ValidateSourceConfigurationSave(save); err != nil {
		return inventory.SourceSaveResult{}, fmt.Errorf("%w: %v", ErrInventoryPersistenceInvalid, err)
	}
	if err := r.available(ctx); err != nil || r.secrets == nil {
		if err != nil {
			return inventory.SourceSaveResult{}, err
		}
		return inventory.SourceSaveResult{}, ErrInventoryPersistenceUnavailable
	}
	configuration := inventory.CanonicalSourceConfiguration(save.Configuration)
	endpoint, _ := inventory.NormalizeExternalURL(save.EndpointURL)
	fingerprint := sourceConfigurationSaveFingerprint(configuration, endpoint, save)
	tx, err := r.pool.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return inventory.SourceSaveResult{}, classifyInventoryError(ctx, err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	// Lock by source so replacement of the selected-library set is serializable.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, configuration.ID); err != nil {
		return inventory.SourceSaveResult{}, classifyInventoryError(ctx, err)
	}
	// Audit IDs make retries idempotent. They are also locked so two concurrent
	// retries cannot independently pass the absence check.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, save.AuditID); err != nil {
		return inventory.SourceSaveResult{}, classifyInventoryError(ctx, err)
	}
	var auditSource string
	var auditRevision int64
	var auditFingerprint []byte
	err = tx.QueryRow(ctx, `SELECT source_id,configuration_revision,command_fingerprint FROM inventory_source_configuration_audit WHERE id=$1`, save.AuditID).Scan(&auditSource, &auditRevision, &auditFingerprint)
	if err == nil {
		if auditSource != configuration.ID || string(auditFingerprint) != string(fingerprint[:]) {
			return inventory.SourceSaveResult{}, inventory.ErrSourceSaveReplayConflict
		}
		if err = tx.Commit(ctx); err != nil {
			return inventory.SourceSaveResult{}, classifyInventoryError(ctx, err)
		}
		return inventory.SourceSaveResult{Revision: auditRevision, AuditReplayed: true}, nil
	}
	if err != nil && err != pgx.ErrNoRows {
		return inventory.SourceSaveResult{}, classifyInventoryError(ctx, err)
	}

	var sourceKind string
	var sourceBaseURL, sourceSecretRef *string
	err = tx.QueryRow(ctx, `SELECT kind,base_url,secret_ref FROM library_sources WHERE id=$1 FOR UPDATE`, configuration.ID).Scan(&sourceKind, &sourceBaseURL, &sourceSecretRef)
	if err == pgx.ErrNoRows {
		if save.ExpectedRevision != 0 {
			return inventory.SourceSaveResult{}, &inventory.SourceNotFoundError{SourceID: configuration.ID}
		}
		if _, err = tx.Exec(ctx, `INSERT INTO library_sources(id,kind,name,base_url,enabled,authoritative_media_types,secret_ref,created_at,updated_at)
			VALUES ($1,$2,$3,NULL,$4,$5::jsonb,NULL,transaction_timestamp(),transaction_timestamp())`,
			configuration.ID, configuration.Kind, configuration.Name, configuration.Enabled, mediaTypesJSON(configuration.MediaTypes)); err != nil {
			return inventory.SourceSaveResult{}, classifyInventoryError(ctx, err)
		}
	} else if err != nil {
		return inventory.SourceSaveResult{}, classifyInventoryError(ctx, err)
	} else if sourceKind != string(configuration.Kind) || sourceBaseURL != nil || sourceSecretRef != nil {
		return inventory.SourceSaveResult{}, ErrInventorySourceLegacy
	}

	var previousRevision int64
	err = tx.QueryRow(ctx, `SELECT revision FROM inventory_source_configurations WHERE source_id=$1 FOR UPDATE`, configuration.ID).Scan(&previousRevision)
	previouslyConfigured := err == nil
	if err != nil && err != pgx.ErrNoRows {
		return inventory.SourceSaveResult{}, classifyInventoryError(ctx, err)
	}
	if previouslyConfigured && save.ExpectedRevision != previousRevision || !previouslyConfigured && save.ExpectedRevision != 0 {
		return inventory.SourceSaveResult{}, inventory.ErrSourceRevisionConflict
	}
	var status string
	err = tx.QueryRow(ctx, `SELECT state FROM inventory_source_status WHERE source_id=$1 FOR UPDATE`, configuration.ID).Scan(&status)
	if err != nil && err != pgx.ErrNoRows {
		return inventory.SourceSaveResult{}, classifyInventoryError(ctx, err)
	}
	if status == string(inventory.SourceSyncRunning) {
		return inventory.SourceSaveResult{}, inventory.ErrSourceReconfigurationBusy
	}
	var revision int64
	if previouslyConfigured {
		err = tx.QueryRow(ctx, `UPDATE inventory_source_configurations SET endpoint_url=$2,revision=revision+1,updated_at=transaction_timestamp()
			WHERE source_id=$1 AND revision=$3 RETURNING revision`, configuration.ID, endpoint, save.ExpectedRevision).Scan(&revision)
	} else {
		err = tx.QueryRow(ctx, `INSERT INTO inventory_source_configurations(source_id,endpoint_url,revision,updated_at)
			VALUES($1,$2,1,transaction_timestamp()) RETURNING revision`, configuration.ID, endpoint).Scan(&revision)
	}
	if err != nil {
		return inventory.SourceSaveResult{}, classifyInventoryError(ctx, err)
	}
	if _, err = tx.Exec(ctx, `UPDATE library_sources SET name=$2,enabled=$3,authoritative_media_types=$4::jsonb,updated_at=transaction_timestamp()
		WHERE id=$1`, configuration.ID, configuration.Name, configuration.Enabled, mediaTypesJSON(configuration.MediaTypes)); err != nil {
		return inventory.SourceSaveResult{}, classifyInventoryError(ctx, err)
	}
	if _, err = tx.Exec(ctx, `DELETE FROM inventory_source_selected_libraries WHERE source_id=$1`, configuration.ID); err != nil {
		return inventory.SourceSaveResult{}, classifyInventoryError(ctx, err)
	}
	for _, libraryID := range configuration.SelectedLibraries {
		if _, err = tx.Exec(ctx, `INSERT INTO inventory_source_selected_libraries(source_id,remote_library_id) VALUES($1,$2)`, configuration.ID, libraryID); err != nil {
			return inventory.SourceSaveResult{}, classifyInventoryError(ctx, err)
		}
	}
	owner, err := sourceSecretOwner(configuration.ID)
	if err != nil {
		return inventory.SourceSaveResult{}, ErrInventoryPersistenceInvalid
	}
	if save.Credential.Preserve {
		var exists bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM encrypted_secrets WHERE namespace=$1 AND owner_id=$2 AND secret_name=$3)`, inventory.InventoryCredentialNamespace, owner[:], inventory.InventoryCredentialName).Scan(&exists); err != nil {
			return inventory.SourceSaveResult{}, classifyInventoryError(ctx, err)
		}
		if !exists {
			return inventory.SourceSaveResult{}, ErrInventoryPersistenceInvalid
		}
	} else if err = r.secrets.UpsertTx(ctx, tx, secrets.Context{Namespace: inventory.InventoryCredentialNamespace, OwnerID: owner, Name: inventory.InventoryCredentialName}, save.Credential.Replace); err != nil {
		return inventory.SourceSaveResult{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO inventory_source_status(source_id,state,failure_detail) VALUES($1,'idle','') ON CONFLICT(source_id) DO NOTHING`, configuration.ID); err != nil {
		return inventory.SourceSaveResult{}, classifyInventoryError(ctx, err)
	}
	action := "configured"
	if previouslyConfigured {
		action = "updated"
	}
	result := inventory.SourceSaveResult{Revision: revision}
	var credentialIdentity []byte
	if !save.Credential.Preserve {
		credentialIdentity = save.Credential.Replace.Identity[:]
	} else if err = tx.QueryRow(ctx, `SELECT credential_identity FROM encrypted_secrets WHERE namespace=$1 AND owner_id=$2 AND secret_name=$3`, inventory.InventoryCredentialNamespace, owner[:], inventory.InventoryCredentialName).Scan(&credentialIdentity); err != nil {
		return inventory.SourceSaveResult{}, classifyInventoryError(ctx, err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO inventory_source_configuration_audit(id,source_id,action,configuration_revision,command_fingerprint,credential_identity,actor_id,occurred_at) VALUES($1,$2,$3,$4,$5,$6,NULLIF($7,'')::uuid,$8)`, save.AuditID, configuration.ID, action, revision, fingerprint[:], credentialIdentity, save.ActorID, save.OccurredAt.UTC()); err != nil {
		return inventory.SourceSaveResult{}, classifyInventoryError(ctx, err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO inventory_source_configuration_revisions(source_id,revision,source_kind,source_name,endpoint_url,enabled,enabled_media_types,credential_identity,audit_id,recorded_at)
		VALUES($1,$2,$3,$4,$5,$6,$7::jsonb,$8,$9,$10)`, configuration.ID, revision, configuration.Kind, configuration.Name, endpoint, configuration.Enabled, mediaTypesJSON(configuration.MediaTypes), credentialIdentity, save.AuditID, save.OccurredAt.UTC()); err != nil {
		return inventory.SourceSaveResult{}, classifyInventoryError(ctx, err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO inventory_source_configuration_credential_revisions(source_id,revision,envelope_version,key_id,nonce,ciphertext,credential_identity)
		SELECT $1,$2,envelope_version,key_id,nonce,ciphertext,credential_identity
		FROM encrypted_secrets WHERE namespace=$3 AND owner_id=$4 AND secret_name=$5`, configuration.ID, revision, inventory.InventoryCredentialNamespace, owner[:], inventory.InventoryCredentialName); err != nil {
		return inventory.SourceSaveResult{}, classifyInventoryError(ctx, err)
	}
	for _, libraryID := range configuration.SelectedLibraries {
		if _, err = tx.Exec(ctx, `INSERT INTO inventory_source_configuration_audit_selected_libraries(audit_id,remote_library_id) VALUES($1,$2)`, save.AuditID, libraryID); err != nil {
			return inventory.SourceSaveResult{}, classifyInventoryError(ctx, err)
		}
		if _, err = tx.Exec(ctx, `INSERT INTO inventory_source_configuration_revision_selected_libraries(source_id,revision,remote_library_id) VALUES($1,$2,$3)`, configuration.ID, revision, libraryID); err != nil {
			return inventory.SourceSaveResult{}, classifyInventoryError(ctx, err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return inventory.SourceSaveResult{}, classifyInventoryError(ctx, err)
	}
	return result, nil
}

// sourceSaveFingerprint binds an audit ID to a canonical, bounded command
// identity. It includes the key-derived credential identity, never plaintext or
// randomized envelope material.
func sourceSaveFingerprint(configuration inventory.SourceConfiguration, endpoint string, save inventory.SourceSave) [sha256.Size]byte {
	return sourceConfigurationSaveFingerprint(configuration, endpoint, inventory.SourceConfigurationSave{Configuration: save.Configuration, EndpointURL: save.EndpointURL, Credential: inventory.SourceCredentialChange{Replace: save.Credential}, ExpectedRevision: save.ExpectedRevision, ActorID: save.ActorID, AuditID: save.AuditID, OccurredAt: save.OccurredAt})
}

func sourceConfigurationSaveFingerprint(configuration inventory.SourceConfiguration, endpoint string, save inventory.SourceConfigurationSave) [sha256.Size]byte {
	var data []byte
	appendString := func(value string) {
		var length [4]byte
		binary.BigEndian.PutUint32(length[:], uint32(len(value)))
		data = append(data, length[:]...)
		data = append(data, value...)
	}
	appendString(configuration.ID)
	appendString(configuration.Name)
	appendString(string(configuration.Kind))
	appendString(endpoint)
	appendString(fmt.Sprintf("%t:%t:%t", configuration.Enabled, configuration.Capabilities.CompleteEnumeration, configuration.Capabilities.Delta))
	for _, mediaType := range configuration.MediaTypes {
		appendString(string(mediaType))
	}
	for _, libraryID := range configuration.SelectedLibraries {
		appendString(libraryID)
	}
	appendString(fmt.Sprintf("%d", save.ExpectedRevision))
	appendString(save.ActorID)
	appendString(save.OccurredAt.UTC().Format(time.RFC3339Nano))
	appendString(fmt.Sprintf("preserve:%t", save.Credential.Preserve))
	if !save.Credential.Preserve {
		appendString(fmt.Sprintf("%d", save.Credential.Replace.Version))
		data = append(data, save.Credential.Replace.Identity[:]...)
	}
	return sha256.Sum256(data)
}

func sourceSecretOwner(id string) (secrets.OwnerID, error) {
	var owner secrets.OwnerID
	hex := strings.ReplaceAll(id, "-", "")
	if len(hex) != 32 {
		return owner, fmt.Errorf("invalid UUID")
	}
	for i := range owner {
		if _, err := fmt.Sscanf(hex[i*2:i*2+2], "%02x", &owner[i]); err != nil {
			return secrets.OwnerID{}, err
		}
	}
	return owner, nil
}

func (r *SourceConfigurationRepository) available(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%w: %v", ErrInventoryPersistenceUnavailable, err)
	}
	if r == nil || r.pool == nil || r.pool.pool == nil {
		return ErrInventoryPersistenceUnavailable
	}
	return nil
}

func mediaTypesJSON(mediaTypes any) string {
	encoded, _ := json.Marshal(mediaTypes)
	return string(encoded)
}

var _ inventory.SourceSaveRepository = (*SourceConfigurationRepository)(nil)
var _ inventory.SourceConfigurationSaveRepository = (*SourceConfigurationRepository)(nil)
