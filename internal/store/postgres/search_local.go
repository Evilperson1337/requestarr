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
// identifier, so items nobody has opened still count. For MusicBrainz types it
// also follows the catalog item's aliases (Plex release and release-track ids),
// which exist only once the item has been opened: a never-opened album or song
// held under such an id has no local state, which means unknown, not absent. A source that has never
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
	), hits AS (
		-- The library stores the item's own identifier (TMDB, IGDB, Navidrome's MusicBrainz id).
		SELECT k.media_type,k.provider,k.provider_id,i.source_id,i.last_seen_at
		FROM k JOIN inventory_items i ON i.media_type=k.media_type AND i.external_ids @> jsonb_build_object(k.provider,k.provider_id)
		UNION
		-- Plex files the canonical MusicBrainz id under "mbid" when it holds that very entity.
		SELECT k.media_type,k.provider,k.provider_id,i.source_id,i.last_seen_at
		FROM k JOIN inventory_items i ON i.media_type=k.media_type AND i.external_ids @> jsonb_build_object('mbid',k.provider_id)
		WHERE k.provider='musicbrainz'
		UNION
		-- Any other identifier recorded as an alias of the catalog item (D020): a
		-- Plex release or release-track id. Only an item that has a catalog row,
		-- and so aliases, can be found this way.
		SELECT k.media_type,k.provider,k.provider_id,i.source_id,i.last_seen_at
		FROM k JOIN catalog_items c ON c.media_type=k.media_type AND c.provider=k.provider AND c.provider_id=k.provider_id
		JOIN catalog_item_aliases a ON a.catalog_item_id=c.id
		JOIN inventory_items i ON i.media_type=k.media_type AND i.external_ids @> jsonb_build_object(a.kind,a.value)
	), lib AS (
		SELECT DISTINCT h.media_type,h.provider,h.provider_id
		FROM hits h JOIN library_sources src ON src.id=h.source_id AND src.enabled
		WHERE h.last_seen_at >= (SELECT max(observed_at) FROM source_snapshot_runs r WHERE r.source_id=h.source_id)
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
