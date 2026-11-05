//go:build integration

package postgres

import (
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/inventory"
)

func key(media domain.MediaType, id string) domain.SearchKey {
	return domain.SearchKey{MediaType: media, Provider: "tmdb", ProviderID: id}
}

// A library item shows as held with no catalog row, movie and TV IDs are
// separate namespaces, and a never-seen item has no local state at all.
func TestSearchLocalStatesLibraryItemWithoutCatalogRowIntegration(t *testing.T) {
	ctx, s, _ := integrationStore(t)
	d := inventory.Descriptor{ID: newUUID(), Kind: "plex", Name: "Plex", MediaTypes: []domain.MediaType{domain.MediaMovie, domain.MediaTV}, IdentifierKinds: []string{"tmdb"}}
	id := newUUID()[:8]
	snap := inventory.Snapshot{SourceID: d.ID, Complete: true, ObservedAt: time.Now().UTC(), Items: []inventory.Item{
		{SourceID: d.ID, SourceItemID: "m-" + id, MediaType: domain.MediaMovie, Title: "Held movie", ExternalIDs: map[string]string{"tmdb": id}},
	}}
	if err := s.PersistSnapshot(ctx, d, "http://plex.example.test", snap); err != nil {
		t.Fatal(err)
	}
	got, err := s.SearchLocalStates(ctx, []domain.SearchKey{key(domain.MediaMovie, id), key(domain.MediaTV, id), key(domain.MediaMovie, id+"0")})
	if err != nil {
		t.Fatal(err)
	}
	local, ok := got[key(domain.MediaMovie, id)]
	if !ok || local.Availability != domain.AvailabilityAvailable || local.CatalogID != "" || local.RequestStatus != "" {
		t.Fatalf("movie = %+v ok=%v", local, ok)
	}
	if _, ok := got[key(domain.MediaTV, id)]; ok {
		t.Fatal("a TV show with the movie's numeric ID must not match")
	}
	if _, ok := got[key(domain.MediaMovie, id+"0")]; ok {
		t.Fatal("a never-seen item must have no local state")
	}
}
