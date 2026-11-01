package postgres

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/requests"
	"github.com/jackc/pgx/v5"
)

// MoviePreview reads local state only. Request creation/subscription still use
// their existing transactional, audited write paths, including race handling.
func (s *Store) MoviePreview(ctx context.Context, catalogID, userID string) (domain.MoviePreview, error) {
	var state domain.MoviePreview
	var request domain.PreviewRequest
	var selection []byte
	err := s.pool.QueryRow(ctx, `SELECT c.availability,COALESCE(r.id::text,''),COALESCE(r.status,''),EXISTS(SELECT 1 FROM request_subscriptions sub WHERE sub.request_id=r.id AND sub.user_id=$2),COALESCE(r.selection,'{}'::jsonb)
	 FROM catalog_items c LEFT JOIN requests r ON r.catalog_item_id=c.id WHERE c.id=$1`, catalogID, userID).Scan(&state.Availability, &request.ID, &request.Status, &request.Subscribed, &selection)
	if errors.Is(err, pgx.ErrNoRows) {
		return state, requests.ErrCatalogNotFound
	}
	if err != nil {
		return state, err
	}
	if request.ID != "" {
		if request.Selection, err = parseSelection(selection); err != nil {
			return state, err
		}
		state.Request = &request
	}
	if state.Availability != domain.AvailabilityAvailable {
		return state, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT s.name,COALESCE(i.external_url,'') FROM availability_evidence e JOIN library_sources s ON s.id=e.source_id JOIN inventory_items i ON i.source_id=e.source_id AND i.source_item_id=e.source_item_id WHERE e.catalog_item_id=$1 AND s.enabled ORDER BY s.name LIMIT 10`, catalogID)
	if err != nil {
		return state, err
	}
	defer rows.Close()
	for rows.Next() {
		var name, raw string
		if err := rows.Scan(&name, &raw); err != nil {
			return state, err
		}
		if safeLibraryLink(raw) {
			state.Libraries = append(state.Libraries, domain.LibraryLink{Name: name, URL: raw})
		}
	}
	return state, rows.Err()
}

func safeLibraryLink(raw string) bool {
	if len(raw) > 2048 || strings.ContainsAny(raw, "\r\n\x00") {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return false
	}
	values, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return false
	}
	// Client navigation IDs are allowed, authentication material is not. Query
	// parsing decodes escaped names, so %74oken cannot bypass this boundary.
	for key := range values {
		switch strings.ToLower(key) {
		case "id", "serverid":
		default:
			return false
		}
	}
	decoded, err := url.QueryUnescape(raw)
	if err != nil || strings.ContainsAny(decoded, "\r\n\x00") {
		return false
	}
	for _, secret := range []string{"token", "api_key", "apikey", "authorization", "password", "secret"} {
		if strings.Contains(strings.ToLower(decoded), secret) {
			return false
		}
	}
	return true
}
