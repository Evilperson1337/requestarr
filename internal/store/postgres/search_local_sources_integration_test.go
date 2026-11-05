//go:build integration

package postgres

import (
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/inventory"
	"github.com/evilperson1337/requestarr/internal/requests"
)

// An open Request is reported with its catalog ID and status, and a resolved
// item no library holds is not available.
func TestSearchLocalStatesReportsRequestAndCatalogIDIntegration(t *testing.T) {
	ctx, s, user := integrationStore(t)
	id := newUUID()[:8]
	item, err := s.Upsert(ctx, domain.ResolvedItem{Item: domain.CatalogItem{MediaType: domain.MediaTV, Provider: "tmdb", ProviderID: id, Title: "Requested show"}})
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.Upsert(ctx, domain.ResolvedItem{Item: domain.CatalogItem{MediaType: domain.MediaMovie, Provider: "tmdb", ProviderID: id, Title: "Same number, movie"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := requests.New(s).Create(ctx, requests.CreateInput{Actor: requests.Actor{ID: user, Role: "requester"}, CatalogID: item.ID}); err != nil {
		t.Fatal(err)
	}
	got, err := s.SearchLocalStates(ctx, []domain.SearchKey{key(domain.MediaTV, id), key(domain.MediaMovie, id)})
	if err != nil {
		t.Fatal(err)
	}
	if l := got[key(domain.MediaTV, id)]; l.CatalogID != item.ID || l.RequestStatus != domain.RequestRequested || l.Availability == domain.AvailabilityAvailable {
		t.Fatalf("tv = %+v", l)
	}
	if l := got[key(domain.MediaMovie, id)]; l.CatalogID != other.ID || l.RequestStatus != "" {
		t.Fatalf("movie must not inherit the show's request: %+v", l)
	}
}

// Only an enabled source's latest complete snapshot holds an item: a disabled
// source, an item since removed, and a source with no completed snapshot hold nothing.
func TestSearchLocalStatesIgnoresDisabledStaleAndUnsyncedSourcesIntegration(t *testing.T) {
	ctx, s, _ := integrationStore(t)
	d := inventory.Descriptor{ID: newUUID(), Kind: "plex", Name: "Plex", MediaTypes: []domain.MediaType{domain.MediaMovie}, IdentifierKinds: []string{"tmdb"}}
	keep, gone, off := newUUID()[:8], newUUID()[:8], newUUID()[:8]
	item := func(id string) inventory.Item {
		return inventory.Item{SourceID: d.ID, SourceItemID: "i-" + id, MediaType: domain.MediaMovie, Title: id, ExternalIDs: map[string]string{"tmdb": id}}
	}
	first := inventory.Snapshot{SourceID: d.ID, Complete: true, ObservedAt: time.Now().Add(-time.Minute).UTC(), Items: []inventory.Item{item(keep), item(gone)}}
	if err := s.PersistSnapshot(ctx, d, "http://plex.example.test", first); err != nil {
		t.Fatal(err)
	}
	second := first
	second.ObservedAt = time.Now().UTC()
	second.Items = []inventory.Item{item(keep)}
	if err := s.PersistSnapshot(ctx, d, "http://plex.example.test", second); err != nil {
		t.Fatal(err)
	}
	// A source that never finished a sync: its rows exist but no snapshot run does.
	unsynced := newUUID()
	if _, err := s.pool.Exec(ctx, `INSERT INTO library_sources(id,kind,name,base_url,enabled,authoritative_media_types,created_at,updated_at) VALUES($1,'plex','Unsynced','http://u.example.test',true,'[]'::jsonb,now(),now())`, unsynced); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO inventory_items(id,source_id,source_item_id,media_type,title,external_ids,first_seen_at,last_seen_at) VALUES($1,$2,'u-1','movie','Unsynced',jsonb_build_object('tmdb',$3::text),now(),now())`, newUUID(), unsynced, off); err != nil {
		t.Fatal(err)
	}
	keys := []domain.SearchKey{key(domain.MediaMovie, keep), key(domain.MediaMovie, gone), key(domain.MediaMovie, off)}
	got, err := s.SearchLocalStates(ctx, keys)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got[keys[0]]; !ok {
		t.Fatal("an item in the latest snapshot must be held")
	}
	if _, ok := got[keys[1]]; ok {
		t.Fatal("an item dropped from the latest snapshot must not be held")
	}
	if _, ok := got[keys[2]]; ok {
		t.Fatal("a source with no completed snapshot must hold nothing")
	}
	// Disabled: a resolved catalog row stays visible but no longer reads as held.
	cat, err := s.Upsert(ctx, domain.ResolvedItem{Item: domain.CatalogItem{MediaType: domain.MediaMovie, Provider: "tmdb", ProviderID: keep, Title: keep}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE library_sources SET enabled=false WHERE id=$1`, d.ID); err != nil {
		t.Fatal(err)
	}
	got, err = s.SearchLocalStates(ctx, keys[:1])
	if err != nil {
		t.Fatal(err)
	}
	if l, ok := got[keys[0]]; !ok || l.CatalogID != cat.ID || l.Availability == domain.AvailabilityAvailable {
		t.Fatalf("disabled source: %+v ok=%v", l, ok)
	}
}
