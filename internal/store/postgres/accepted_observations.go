package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"git.example.com/evilperson1337/Requestarr/internal/domain"
	"git.example.com/evilperson1337/Requestarr/internal/inventory"
)

// CommitAcceptedObservation durably accepts only a complete proven observation.
// It locks the source before looking up the command so concurrent commits cannot
// derive projection changes from stale state.
func (r *InventoryRepository) CommitAcceptedObservation(ctx context.Context, command inventory.AcceptedObservationCommit) (inventory.AcceptedObservationCommitResult, error) {
	if err := inventory.ValidateAcceptedObservationCommit(command); err != nil {
		return inventory.AcceptedObservationCommitResult{}, fmt.Errorf("%w: %v", ErrInventoryPersistenceInvalid, err)
	}
	if err := r.available(ctx); err != nil {
		return inventory.AcceptedObservationCommitResult{}, err
	}
	fingerprint, err := inventory.AcceptedObservationFingerprint(command)
	if err != nil {
		return inventory.AcceptedObservationCommitResult{}, fmt.Errorf("%w: %v", ErrInventoryPersistenceInvalid, err)
	}
	tx, err := r.pool.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return inventory.AcceptedObservationCommitResult{}, classifyInventoryError(ctx, err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	scope := command.Observation.Scope
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, scope.SourceID); err != nil {
		return inventory.AcceptedObservationCommitResult{}, classifyInventoryError(ctx, err)
	}
	var revisionExists bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM inventory_source_configuration_revisions WHERE source_id=$1 AND revision=$2)`, scope.SourceID, scope.ConfigurationRevision).Scan(&revisionExists); err != nil || !revisionExists {
		return inventory.AcceptedObservationCommitResult{}, inventory.ErrObservationScopeMismatch
	}
	if err = verifyAcceptedScope(ctx, tx, scope); err != nil {
		return inventory.AcceptedObservationCommitResult{}, err
	}
	var storedFingerprint []byte
	var added, updated, removed int
	err = tx.QueryRow(ctx, `SELECT command_fingerprint FROM inventory_accepted_observations WHERE id=$1`, command.ID).Scan(&storedFingerprint)
	if err == nil {
		if string(storedFingerprint) != string(fingerprint[:]) {
			return inventory.AcceptedObservationCommitResult{}, inventory.ErrObservationCommitConflict
		}
		if err = tx.QueryRow(ctx, `SELECT count(*) FILTER (WHERE change_kind='added'),count(*) FILTER (WHERE change_kind='updated'),count(*) FILTER (WHERE change_kind='removed') FROM inventory_accepted_observation_events WHERE observation_id=$1`, command.ID).Scan(&added, &updated, &removed); err != nil {
			return inventory.AcceptedObservationCommitResult{}, classifyInventoryError(ctx, err)
		}
		if err = tx.Commit(ctx); err != nil {
			return inventory.AcceptedObservationCommitResult{}, classifyInventoryError(ctx, err)
		}
		return inventory.AcceptedObservationCommitResult{ID: command.ID, Added: added, Updated: updated, Removed: removed}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return inventory.AcceptedObservationCommitResult{}, classifyInventoryError(ctx, err)
	}
	known, err := loadDesiredState(ctx, tx, scope)
	if err != nil {
		return inventory.AcceptedObservationCommitResult{}, err
	}
	diff, err := inventory.DiffCompleteObservation(command.Observation, known)
	if err != nil {
		return inventory.AcceptedObservationCommitResult{}, fmt.Errorf("%w: %v", ErrInventoryPersistenceInvalid, err)
	}
	// PostgreSQL owns acceptance time, ordering, and predecessor lineage. The
	// insert deliberately supplies only command-owned evidence fields so the
	// metadata trigger can atomically derive the next transition position.
	if _, err = tx.Exec(ctx, `INSERT INTO inventory_accepted_observations(id,source_id,configuration_revision,command_fingerprint) VALUES($1,$2,$3,$4)`, command.ID, scope.SourceID, scope.ConfigurationRevision, fingerprint[:]); err != nil {
		return inventory.AcceptedObservationCommitResult{}, classifyInventoryError(ctx, err)
	}
	for _, coverage := range command.Observation.Coverage {
		if _, err = tx.Exec(ctx, `INSERT INTO inventory_accepted_observation_coverage(observation_id,remote_library_id,media_type,coverage_status) VALUES($1,$2,$3,$4)`, command.ID, coverage.RemoteLibraryID, coverage.MediaType, coverage.Status); err != nil {
			return inventory.AcceptedObservationCommitResult{}, classifyInventoryError(ctx, err)
		}
	}
	for _, item := range command.Observation.Items {
		if _, err = tx.Exec(ctx, `INSERT INTO inventory_accepted_observation_items(observation_id,remote_library_id,source_item_id,media_type,title,external_url,item_revision) VALUES($1,$2,$3,$4,$5,$6,$7)`, command.ID, item.RemoteLibraryID, item.ItemID, item.MediaType, item.Title, item.ExternalURL, item.Revision); err != nil {
			return inventory.AcceptedObservationCommitResult{}, classifyInventoryError(ctx, err)
		}
	}
	ordinal := 0
	write := func(kind inventory.ChangeKind, items []inventory.ObservedItem) error {
		for _, item := range items {
			ordinal++
			if _, err := tx.Exec(ctx, `INSERT INTO inventory_accepted_observation_events(observation_id,ordinal,change_kind,remote_library_id,source_item_id,media_type) VALUES($1,$2,$3,$4,$5,$6)`, command.ID, ordinal, kind, item.RemoteLibraryID, item.ItemID, item.MediaType); err != nil {
				return err
			}
		}
		return nil
	}
	if err = write(inventory.ChangeAdded, diff.Added); err == nil {
		err = write(inventory.ChangeUpdated, diff.Updated)
	}
	if err == nil {
		err = write(inventory.ChangeRemoved, diff.Removed)
	}
	if err != nil {
		return inventory.AcceptedObservationCommitResult{}, classifyInventoryError(ctx, err)
	}
	for _, item := range append(diff.Added, diff.Updated...) {
		if _, err = tx.Exec(ctx, `INSERT INTO inventory_desired_source_revision_items(source_id,configuration_revision,remote_library_id,source_item_id,media_type,title,external_url,item_revision,last_observation_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(source_id,configuration_revision,remote_library_id,source_item_id) DO UPDATE SET media_type=EXCLUDED.media_type,title=EXCLUDED.title,external_url=EXCLUDED.external_url,item_revision=EXCLUDED.item_revision,last_observation_id=EXCLUDED.last_observation_id`, scope.SourceID, scope.ConfigurationRevision, item.RemoteLibraryID, item.ItemID, item.MediaType, item.Title, item.ExternalURL, item.Revision, command.ID); err != nil {
			return inventory.AcceptedObservationCommitResult{}, classifyInventoryError(ctx, err)
		}
	}
	for _, item := range diff.Removed {
		if _, err = tx.Exec(ctx, `DELETE FROM inventory_desired_source_revision_items WHERE source_id=$1 AND configuration_revision=$2 AND remote_library_id=$3 AND source_item_id=$4`, scope.SourceID, scope.ConfigurationRevision, item.RemoteLibraryID, item.ItemID); err != nil {
			return inventory.AcceptedObservationCommitResult{}, classifyInventoryError(ctx, err)
		}
	}
	// The projection represents the newly accepted complete observation, not
	// just the rows whose values changed. This makes provenance advance for
	// unchanged rows as well.
	if _, err = tx.Exec(ctx, `UPDATE inventory_desired_source_revision_items SET last_observation_id=$3 WHERE source_id=$1 AND configuration_revision=$2`, scope.SourceID, scope.ConfigurationRevision, command.ID); err != nil {
		return inventory.AcceptedObservationCommitResult{}, classifyInventoryError(ctx, err)
	}
	if err = tx.Commit(ctx); err != nil {
		return inventory.AcceptedObservationCommitResult{}, classifyInventoryError(ctx, err)
	}
	return inventory.AcceptedObservationCommitResult{ID: command.ID, Applied: true, Added: len(diff.Added), Updated: len(diff.Updated), Removed: len(diff.Removed)}, nil
}

func verifyAcceptedScope(ctx context.Context, tx pgx.Tx, scope inventory.ObservationScope) error {
	var libraries []string
	rows, err := tx.Query(ctx, `SELECT remote_library_id FROM inventory_source_configuration_revision_selected_libraries WHERE source_id=$1 AND revision=$2 ORDER BY remote_library_id`, scope.SourceID, scope.ConfigurationRevision)
	if err != nil {
		return classifyInventoryError(ctx, err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return classifyInventoryError(ctx, err)
		}
		libraries = append(libraries, id)
	}
	if err := rows.Err(); err != nil {
		return classifyInventoryError(ctx, err)
	}
	var persistedMediaTypes []string
	if err = tx.QueryRow(ctx, `SELECT ARRAY(SELECT jsonb_array_elements_text(enabled_media_types)) FROM inventory_source_configuration_revisions WHERE source_id=$1 AND revision=$2`, scope.SourceID, scope.ConfigurationRevision).Scan(&persistedMediaTypes); err != nil {
		return inventory.ErrObservationScopeMismatch
	}
	mediaTypes := make([]domain.MediaType, len(persistedMediaTypes))
	for i, mediaType := range persistedMediaTypes {
		mediaTypes[i] = domain.MediaType(mediaType)
	}
	actual := inventory.ObservationScope{SourceID: scope.SourceID, ConfigurationRevision: scope.ConfigurationRevision, SelectedLibraries: libraries, EnabledMediaTypes: mediaTypes}
	if _, err := inventory.DiffCompleteObservation(inventory.CompleteObservation{Scope: scope, Coverage: fullCoverage(scope)}, inventory.KnownInventoryState{Scope: actual}); err != nil {
		return inventory.ErrObservationScopeMismatch
	}
	return nil
}

func fullCoverage(scope inventory.ObservationScope) []inventory.ObservationCoverage {
	result := make([]inventory.ObservationCoverage, 0, len(scope.SelectedLibraries)*len(scope.EnabledMediaTypes))
	for _, library := range scope.SelectedLibraries {
		for _, mediaType := range scope.EnabledMediaTypes {
			result = append(result, inventory.ObservationCoverage{RemoteLibraryID: library, MediaType: mediaType, Status: inventory.CoverageVerifiedComplete})
		}
	}
	return result
}

func loadDesiredState(ctx context.Context, tx pgx.Tx, scope inventory.ObservationScope) (inventory.KnownInventoryState, error) {
	state := inventory.KnownInventoryState{Scope: scope}
	rows, err := tx.Query(ctx, `SELECT remote_library_id,source_item_id,media_type,title,external_url,item_revision FROM inventory_desired_source_revision_items WHERE source_id=$1 AND configuration_revision=$2 ORDER BY remote_library_id,source_item_id`, scope.SourceID, scope.ConfigurationRevision)
	if err != nil {
		return state, classifyInventoryError(ctx, err)
	}
	defer rows.Close()
	for rows.Next() {
		var item inventory.ObservedItem
		if err := rows.Scan(&item.RemoteLibraryID, &item.ItemID, &item.MediaType, &item.Title, &item.ExternalURL, &item.Revision); err != nil {
			return state, classifyInventoryError(ctx, err)
		}
		state.Items = append(state.Items, item)
	}
	if err := rows.Err(); err != nil {
		return state, classifyInventoryError(ctx, err)
	}
	return state, nil
}

var _ inventory.AcceptedObservationRepository = (*InventoryRepository)(nil)
