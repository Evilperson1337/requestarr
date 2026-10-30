//go:build integration

package postgres

import (
	"encoding/json"
	"testing"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/requests"
	"github.com/evilperson1337/requestarr/internal/sources"
)

func TestMoviePreviewPersistenceAndExactRequestNavigationIntegration(t *testing.T) {
	ctx, s, user := setupDraftStore(t)
	other := integrationUser(t, ctx, s)
	item, err := s.Upsert(ctx, domain.ResolvedItem{Item: domain.CatalogItem{MediaType: domain.MediaMovie, Provider: "tmdb", ProviderID: "1762506", Title: "Preview fixture", Metadata: map[string]any{"movie": domain.MovieDetails{Runtime: 145, Genres: []string{"Adventure"}, Directors: []string{"Fixture Director"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	state, err := s.MoviePreview(ctx, item.ID, user)
	if err != nil || state.Request != nil || state.Availability != domain.AvailabilityUnknown {
		t.Fatalf("new preview: %+v error=%v", state, err)
	}
	request, created, err := s.CreateOrGet(ctx, domain.Request{CatalogItemID: item.ID, RequestedBy: user, Status: domain.RequestRequested, Platforms: domain.PlatformSelection{Any: true}}, domain.RequestSubscription{UserID: user, Source: domain.SubscriptionRequester})
	if err != nil || !created {
		t.Fatal("could not create fixture Request")
	}
	s.Close()
	s, err = Open(ctx, testDatabaseURL(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var raw []byte
	if err := s.pool.QueryRow(ctx, `SELECT metadata->'movie' FROM catalog_items WHERE id=$1`, item.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var movie domain.MovieDetails
	if json.Unmarshal(raw, &movie) != nil || movie.Runtime != 145 || len(movie.Directors) != 1 {
		t.Fatal("movie metadata did not survive reopening the store")
	}
	state, err = s.MoviePreview(ctx, item.ID, user)
	if err != nil || state.Request == nil || state.Request.ID != request.ID || !state.Request.Subscribed {
		t.Fatal("creator subscription not reflected in preview")
	}
	state, err = s.MoviePreview(ctx, item.ID, other)
	if err != nil || state.Request == nil || state.Request.Subscribed {
		t.Fatal("preview leaked another user's subscription state")
	}
	page, err := s.List(ctx, user, requests.ListQuery{RequestID: request.ID})
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != request.ID {
		t.Fatalf("exact Request navigation failed: %v", err)
	}
	if _, err = s.pool.Exec(ctx, `UPDATE catalog_items SET availability='available' WHERE id=$1`, item.ID); err != nil {
		t.Fatal(err)
	}
	state, err = s.MoviePreview(ctx, item.ID, user)
	if err != nil || state.Availability != domain.AvailabilityAvailable || len(state.Libraries) != 0 {
		t.Fatal("availability without an actual library URL was misrepresented")
	}
	sourceID := newUUID()
	if _, err = s.pool.Exec(ctx, `INSERT INTO library_sources(id,kind,name,base_url,enabled,authoritative_media_types,created_at,updated_at) VALUES($1,$2,'Fixture library','https://library.example.test',true,'["movie"]',now(),now())`, sourceID, sources.All()[0].Slug); err != nil {
		t.Fatal(err)
	}
	if _, err = s.pool.Exec(ctx, `INSERT INTO inventory_items(id,source_id,source_item_id,media_type,title,external_url,first_seen_at,last_seen_at) VALUES($1,$2,'movie-1','movie','Fixture','https://library.example.test/items/1',now(),now())`, newUUID(), sourceID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.pool.Exec(ctx, `INSERT INTO availability_evidence(catalog_item_id,source_id,source_item_id,observed_at) VALUES($1,$2,'movie-1',now())`, item.ID, sourceID); err != nil {
		t.Fatal(err)
	}
	state, err = s.MoviePreview(ctx, item.ID, user)
	if err != nil || len(state.Libraries) != 1 || state.Libraries[0].URL != "https://library.example.test/items/1" {
		t.Fatal("usable library link was not returned")
	}
	if _, err = s.pool.Exec(ctx, `UPDATE inventory_items SET external_url='https://library.example.test/items/1?%74oken=canary' WHERE source_id=$1`, sourceID); err != nil {
		t.Fatal(err)
	}
	state, err = s.MoviePreview(ctx, item.ID, user)
	if err != nil || len(state.Libraries) != 0 {
		t.Fatal("credential-bearing library link escaped the preview boundary")
	}
}
