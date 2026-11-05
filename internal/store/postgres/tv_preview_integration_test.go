//go:build integration

package postgres

import (
	"encoding/json"
	"testing"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/requests"
)

func TestTVPreviewPersistsSeriesDetailsAndKeepsMovieNamespaceSeparateIntegration(t *testing.T) {
	ctx, s, user := integrationStore(t)
	tv := &domain.TVDetails{Seasons: 3, Episodes: 30, FirstAirDate: "2020-01-02", SeriesStatus: "Returning Series", Creators: []string{"Fixture Creator"}}
	item, err := s.Upsert(ctx, domain.ResolvedItem{Item: domain.CatalogItem{MediaType: domain.MediaTV, Provider: "tmdb", ProviderID: "714", Title: "Fixture series", Metadata: map[string]any{"tv": tv}}, TV: tv})
	if err != nil {
		t.Fatal(err)
	}
	movie, err := s.Upsert(ctx, domain.ResolvedItem{Item: domain.CatalogItem{MediaType: domain.MediaMovie, Provider: "tmdb", ProviderID: "714", Title: "Different movie"}})
	if err != nil || movie.ID == item.ID {
		t.Fatal("movie and TV numeric namespaces were conflated")
	}
	result, err := requests.New(s).Create(ctx, requests.CreateInput{Actor: requests.Actor{ID: user, Role: "requester"}, CatalogID: item.ID, Platforms: domain.PlatformSelection{Any: true}})
	if err != nil || !result.Created || result.Request.CatalogItemID != item.ID {
		t.Fatal("whole-series Request did not use the canonical TV catalog item")
	}
	s.Close()
	s, err = Open(ctx, testDatabaseURL(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var raw []byte
	if err := s.pool.QueryRow(ctx, `SELECT metadata->'tv' FROM catalog_items WHERE id=$1`, item.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var saved domain.TVDetails
	if json.Unmarshal(raw, &saved) != nil || saved.Seasons != 3 || saved.Episodes != 30 || len(saved.Creators) != 1 {
		t.Fatal("typed TV metadata did not survive reopening")
	}
	state, err := s.MoviePreview(ctx, item.ID, user)
	if err != nil || state.Request == nil || state.Request.ID != result.Request.ID || !state.Request.Subscribed || state.Availability != domain.AvailabilityUnknown {
		t.Fatal("TV Request/subscription state was not returned locally")
	}
}
