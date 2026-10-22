package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/evilperson1337/requestarr/internal/domain"
)

// SaveCatalogAliases records alternate identifiers of a catalog item. It is
// idempotent. Callers include the item's own identifier so that "has aliases"
// also means "has been looked up".
func (s *Store) SaveCatalogAliases(ctx context.Context, catalogID, kind string, values []string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin save aliases: %w", err)
	}
	defer tx.Rollback(ctx)
	for _, v := range values {
		if _, err := tx.Exec(ctx, `INSERT INTO catalog_item_aliases(catalog_item_id,kind,value) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, catalogID, kind, v); err != nil {
			return fmt.Errorf("save catalog alias: %w", err)
		}
	}
	return tx.Commit(ctx)
}

// aliasRetryBackoff is how long a failed lookup waits before it is offered again.
const aliasRetryBackoff = time.Hour

// RecordAliasLookupFailure notes a failed lookup so the item queues behind
// others and waits out aliasRetryBackoff.
func (s *Store) RecordAliasLookupFailure(ctx context.Context, catalogID string) error {
	if _, err := s.pool.Exec(ctx, `INSERT INTO catalog_item_alias_attempts(catalog_item_id,last_attempt_at) VALUES($1,now()) ON CONFLICT(catalog_item_id) DO UPDATE SET last_attempt_at=now()`, catalogID); err != nil {
		return fmt.Errorf("record alias lookup failure: %w", err)
	}
	return nil
}

// OpenCatalogItemsNeedingAliases lists music items of the provider that have an
// open (requested or approved) Request and no aliases yet. Items never tried
// come first (oldest Request first), then retries whose backoff has passed.
func (s *Store) OpenCatalogItemsNeedingAliases(ctx context.Context, provider string, limit int) ([]domain.CatalogRef, error) {
	rows, err := s.pool.Query(ctx, `SELECT c.id::text,c.media_type,c.provider,c.provider_id
		FROM catalog_items c JOIN requests r ON r.catalog_item_id=c.id
		WHERE c.provider=$1 AND c.media_type IN ('album','song') AND r.status IN ('requested','approved')
		AND NOT EXISTS (SELECT 1 FROM catalog_item_aliases a WHERE a.catalog_item_id=c.id)
		AND NOT EXISTS (SELECT 1 FROM catalog_item_alias_attempts t WHERE t.catalog_item_id=c.id AND t.last_attempt_at>now()-make_interval(secs=>$3))
		ORDER BY (SELECT t.last_attempt_at FROM catalog_item_alias_attempts t WHERE t.catalog_item_id=c.id) NULLS FIRST,r.requested_at,c.id LIMIT $2`, provider, limit, aliasRetryBackoff.Seconds())
	if err != nil {
		return nil, fmt.Errorf("list catalog items needing aliases: %w", err)
	}
	defer rows.Close()
	var refs []domain.CatalogRef
	for rows.Next() {
		var r domain.CatalogRef
		if err := rows.Scan(&r.ID, &r.MediaType, &r.Provider, &r.ProviderID); err != nil {
			return nil, fmt.Errorf("scan catalog ref: %w", err)
		}
		refs = append(refs, r)
	}
	return refs, rows.Err()
}
