//go:build integration

package postgres

import (
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/inventory"
	"github.com/evilperson1337/requestarr/internal/requests"
)

// An audiobook search row reads "In library" by ASIN when Audiobookshelf holds
// it (the adapter stores the ASIN under the "audible" identifier), and
// "Requested" once someone has requested that ASIN.
func TestSearchLocalStatesAudiobookByASINIntegration(t *testing.T) {
	ctx, s, user := integrationStore(t)
	suffix := newUUID()[:8]
	held, asked, unknown := "B0"+suffix, "B1"+suffix, "B2"+suffix
	d := inventory.Descriptor{ID: newUUID(), Kind: "audiobookshelf", Name: "Audiobookshelf", MediaTypes: []domain.MediaType{domain.MediaAudiobook}, IdentifierKinds: []string{"audible"}}
	snap := inventory.Snapshot{SourceID: d.ID, Complete: true, ObservedAt: time.Now().UTC(), Items: []inventory.Item{
		{SourceID: d.ID, SourceItemID: "li-1", MediaType: domain.MediaAudiobook, Title: "Held book", ExternalIDs: map[string]string{"audible": held}},
	}}
	if err := s.PersistSnapshot(ctx, d, "http://abs.example.test", snap); err != nil {
		t.Fatal(err)
	}
	item, err := s.Upsert(ctx, domain.ResolvedItem{Item: domain.CatalogItem{MediaType: domain.MediaAudiobook, Provider: "audible", ProviderID: asked, Title: "Requested book"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := requests.New(s).Create(ctx, requests.CreateInput{Actor: requests.Actor{ID: user, Role: "requester"}, CatalogID: item.ID}); err != nil {
		t.Fatal(err)
	}
	k := func(asin string) domain.SearchKey {
		return domain.SearchKey{MediaType: domain.MediaAudiobook, Provider: "audible", ProviderID: asin}
	}
	got, err := s.SearchLocalStates(ctx, []domain.SearchKey{k(held), k(asked), k(unknown)})
	if err != nil {
		t.Fatal(err)
	}
	if l, ok := got[k(held)]; !ok || l.Availability != domain.AvailabilityAvailable {
		t.Fatalf("held audiobook: %+v ok=%v", l, ok)
	}
	if l := got[k(asked)]; l.CatalogID != item.ID || l.RequestStatus != domain.RequestRequested || l.Availability == domain.AvailabilityAvailable {
		t.Fatalf("requested audiobook: %+v", l)
	}
	if _, ok := got[k(unknown)]; ok {
		t.Fatal("an unknown ASIN must have no local state")
	}
}
