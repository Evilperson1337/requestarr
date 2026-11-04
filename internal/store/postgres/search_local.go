package postgres

import (
	"context"
	"fmt"

	"github.com/evilperson1337/requestarr/internal/domain"
)

// SearchLocalStates annotates a page of search results from local tables only,
// with one query. A key appears in the result when it has a catalog row or is
// held by a library. Library presence follows the resolve-time rule (an enabled
// source's latest complete snapshot) but matches inventory directly by provider
// identifier, so items nobody has opened still count. A source that has never
// completed a snapshot holds nothing.
func (s *Store) SearchLocalStates(ctx context.Context, keys []domain.SearchKey) (map[domain.SearchKey]domain.SearchLocal, error) {
	out := map[domain.SearchKey]domain.SearchLocal{}
	if len(keys) == 0 {
		return out, nil
	}
	media, provider, id := make([]string, len(keys)), make([]string, len(keys)), make([]string, len(keys))
	for i, k := range keys {
		media[i], provider[i], id[i] = string(k.MediaType), k.Provider, k.ProviderID
	}
	rows, err := s.pool.Query(ctx, `WITH k AS (
		SELECT DISTINCT * FROM unnest($1::text[],$2::text[],$3::text[]) AS t(media_type,provider,provider_id)
	), cat AS (
		SELECT k.media_type,k.provider,k.provider_id,c.id::text AS catalog_id,COALESCE(r.status,'') AS status
		FROM k JOIN catalog_items c ON c.media_type=k.media_type AND c.provider=k.provider AND c.provider_id=k.provider_id
		LEFT JOIN requests r ON r.catalog_item_id=c.id
	), lib AS (
		SELECT DISTINCT k.media_type,k.provider,k.provider_id
		FROM k JOIN inventory_items i ON i.media_type=k.media_type AND i.external_ids @> jsonb_build_object(k.provider,k.provider_id)
		JOIN library_sources src ON src.id=i.source_id AND src.enabled
		WHERE i.last_seen_at >= (SELECT max(observed_at) FROM source_snapshot_runs r WHERE r.source_id=i.source_id)
	)
	SELECT k.media_type,k.provider,k.provider_id,COALESCE(cat.catalog_id,''),COALESCE(cat.status,''),(lib.provider_id IS NOT NULL)
	FROM k LEFT JOIN cat USING (media_type,provider,provider_id) LEFT JOIN lib USING (media_type,provider,provider_id)
	WHERE cat.catalog_id IS NOT NULL OR lib.provider_id IS NOT NULL`, media, provider, id)
	if err != nil {
		return nil, fmt.Errorf("annotate search results: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var k domain.SearchKey
		var m, status string
		var local domain.SearchLocal
		var held bool
		if err := rows.Scan(&m, &k.Provider, &k.ProviderID, &local.CatalogID, &status, &held); err != nil {
			return nil, fmt.Errorf("scan search annotation: %w", err)
		}
		k.MediaType, local.RequestStatus = domain.MediaType(m), domain.RequestStatus(status)
		// Held means an enabled library holds it now; the catalog's own flag can
		// outlive a disabled source, so it is not trusted for that.
		local.Availability = domain.AvailabilityUnknown
		if held {
			local.Availability = domain.AvailabilityAvailable
		}
		out[k] = local
	}
	return out, rows.Err()
}
