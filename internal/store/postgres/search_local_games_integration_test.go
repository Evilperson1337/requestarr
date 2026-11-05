//go:build integration

package postgres

import (
	"strconv"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/inventory"
	"github.com/evilperson1337/requestarr/internal/requests"
)

// A game search row carries IGDB's numeric id as its provider id. A game RomM
// holds (RomM files its numeric IGDB id under the "igdb" identifier) reads "In
// library" without anyone having opened it, and a requested game reads
// "Requested".
func TestSearchLocalStatesGameByNumericIGDBIDIntegration(t *testing.T) {
	ctx, s, user := integrationStore(t)
	// Numeric ids unique to this run, as the search would return them.
	base := time.Now().UnixNano() % 1_000_000_000
	held, asked, unknown := base*10+1, base*10+2, base*10+3
	id := func(n int64) string { return strconv.FormatInt(n, 10) }
	d := inventory.Descriptor{ID: newUUID(), Kind: "romm", Name: "RomM", MediaTypes: []domain.MediaType{domain.MediaGame}, IdentifierKinds: []string{"igdb"}}
	snap := inventory.Snapshot{SourceID: d.ID, Complete: true, ObservedAt: time.Now().UTC(), Items: []inventory.Item{
		{SourceID: d.ID, SourceItemID: "rom-1", MediaType: domain.MediaGame, Title: "Held game", ExternalIDs: map[string]string{"igdb": id(held)}},
	}}
	if err := s.PersistSnapshot(ctx, d, "http://romm.example.test", snap); err != nil {
		t.Fatal(err)
	}
	item, err := s.Upsert(ctx, domain.ResolvedItem{Item: domain.CatalogItem{MediaType: domain.MediaGame, Provider: "igdb", ProviderID: id(asked), Title: "Requested game"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := requests.New(s).Create(ctx, requests.CreateInput{Actor: requests.Actor{ID: user, Role: "requester"}, CatalogID: item.ID, Platforms: domain.PlatformSelection{Any: true}}); err != nil {
		t.Fatal(err)
	}
	k := func(n int64) domain.SearchKey {
		return domain.SearchKey{MediaType: domain.MediaGame, Provider: "igdb", ProviderID: id(n)}
	}
	got, err := s.SearchLocalStates(ctx, []domain.SearchKey{k(held), k(asked), k(unknown)})
	if err != nil {
		t.Fatal(err)
	}
	if l, ok := got[k(held)]; !ok || l.Availability != domain.AvailabilityAvailable {
		t.Fatalf("RomM-held game: %+v ok=%v", l, ok)
	}
	if l := got[k(asked)]; l.CatalogID != item.ID || l.RequestStatus != domain.RequestRequested || l.Availability == domain.AvailabilityAvailable {
		t.Fatalf("requested game: %+v", l)
	}
	if _, ok := got[k(unknown)]; ok {
		t.Fatal("an unknown game must have no local state")
	}
}
