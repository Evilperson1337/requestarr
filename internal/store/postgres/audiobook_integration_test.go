//go:build integration

package postgres

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/inventory"
	"github.com/evilperson1337/requestarr/internal/sources"
)

func TestAudiobookCatalogItemRoundTripsIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	url := os.Getenv("REQUESTARR_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("REQUESTARR_TEST_DATABASE_URL not set")
	}
	s, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	asin := "B0" + newUUID()[:8]
	item, err := s.Upsert(ctx, domain.ResolvedItem{Item: domain.CatalogItem{MediaType: domain.MediaAudiobook, Provider: "audible", ProviderID: asin, Title: "Audiobook integration", Metadata: map[string]any{"region": "us"}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = s.pool.Exec(context.Background(), `DELETE FROM catalog_items WHERE id=$1`, item.ID) })
	if item.MediaType != domain.MediaAudiobook || item.ArtworkKind != domain.ArtworkSquare {
		t.Fatalf("got %q with %q artwork; want audiobook with square", item.MediaType, item.ArtworkKind)
	}
}

func TestUnresolvedAudiobookNeverReplacesResolvedMetadataIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	url := os.Getenv("REQUESTARR_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("REQUESTARR_TEST_DATABASE_URL not set")
	}
	s, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	asin := "B0" + newUUID()[:8]
	item := func(title string, meta map[string]any) domain.ResolvedItem {
		return domain.ResolvedItem{Item: domain.CatalogItem{MediaType: domain.MediaAudiobook, Provider: "audible", ProviderID: asin, Title: title, Year: 2021, Metadata: meta}}
	}
	// A placeholder first, then the real thing, then a placeholder again.
	first, err := s.Upsert(ctx, item("Audiobook "+asin, map[string]any{"unresolved": true}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = s.pool.Exec(context.Background(), `DELETE FROM catalog_items WHERE id=$1`, first.ID) })
	real, err := s.Upsert(ctx, item("The Real Title", map[string]any{"region": "us"}))
	if err != nil || real.ID != first.ID || real.Title != "The Real Title" {
		t.Fatalf("resolved upsert: %+v, %v", real, err)
	}
	again, err := s.Upsert(ctx, item("Audiobook "+asin, map[string]any{"unresolved": true}))
	if err != nil || again.ID != first.ID {
		t.Fatalf("placeholder upsert: %+v, %v", again, err)
	}
	if again.Title != "The Real Title" || again.Year != 2021 || again.Unresolved() {
		t.Fatalf("placeholder replaced resolved metadata: %+v", again)
	}
}

var absDescriptor = inventory.Descriptor{ID: sources.AudiobookshelfID, Kind: "audiobookshelf", Name: "Audiobookshelf", MediaTypes: []domain.MediaType{domain.MediaAudiobook}, IdentifierKinds: []string{"audible"}}

func newAudiobookRequest(t *testing.T, ctx context.Context, s *Store, asin, title string) (catalogID, requestID string) {
	t.Helper()
	item, err := s.Upsert(ctx, domain.ResolvedItem{Item: domain.CatalogItem{MediaType: domain.MediaAudiobook, Provider: "audible", ProviderID: asin, Title: title}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = s.pool.Exec(context.Background(), `DELETE FROM catalog_items WHERE id=$1`, item.ID) })
	user := integrationUser(t, ctx, s)
	req, _, err := s.CreateOrGet(ctx, domain.Request{CatalogItemID: item.ID, Status: domain.RequestRequested, RequestedBy: user, Platforms: domain.PlatformSelection{Any: true}}, domain.RequestSubscription{UserID: user, Source: domain.SubscriptionRequester})
	if err != nil {
		t.Fatal(err)
	}
	return item.ID, req.ID
}

func absSnapshot(at time.Time, items ...inventory.Item) inventory.Snapshot {
	for i := range items {
		items[i].SourceID, items[i].MediaType = absDescriptor.ID, domain.MediaAudiobook
	}
	return inventory.Snapshot{SourceID: absDescriptor.ID, Complete: true, ObservedAt: at, Items: items}
}

// A request by ASIN becomes Available when an Audiobookshelf item carries that
// ASIN, and an item with no ASIN only ever produces a reviewable suggestion.
func TestAudiobookRequestBecomesAvailableByASINIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s := openJobStore(t, ctx)
	asin := "B0" + newUUID()[:8]
	title := "ABS Integration " + asin
	catalogID, requestID := newAudiobookRequest(t, ctx, s, asin, title)
	otherCatalog, otherRequest := newAudiobookRequest(t, ctx, s, "B1"+newUUID()[:8], "Hand matched "+asin)
	now := time.Now().UTC().Truncate(time.Second)
	persistAs(t, ctx, s, absDescriptor, absSnapshot(now,
		inventory.Item{SourceItemID: "li-" + asin, Title: title, ExternalIDs: map[string]string{"audible": asin}},
		inventory.Item{SourceItemID: "li-hand-" + asin, Title: "Hand matched " + asin, ExternalIDs: map[string]string{}},
	))
	if got := requestStatus(t, ctx, s, requestID); got != "available" {
		t.Fatalf("ASIN-matched request is %s", got)
	}
	if got := readAvailability(t, ctx, s, catalogID); got.availability != "available" {
		t.Fatalf("catalog availability %+v", got)
	}
	if got := requestStatus(t, ctx, s, otherRequest); got != "requested" {
		t.Fatalf("an item with no ASIN satisfied a request without review: %s", got)
	}
	if got := readAvailability(t, ctx, s, otherCatalog); got.availability == "available" {
		t.Fatalf("title-only candidate counted as availability: %+v", got)
	}
	suggestions, err := s.ListMatchSuggestions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range suggestions {
		found = found || (m.CatalogItemID == otherCatalog && m.MediaType == "audiobook")
	}
	if !found {
		t.Fatalf("no pending suggestion for the ASIN-less item among %d", len(suggestions))
	}
	// A title resolved after the library synced is Available at once.
	late, err := s.Upsert(ctx, domain.ResolvedItem{Item: domain.CatalogItem{MediaType: domain.MediaAudiobook, Provider: "audible", ProviderID: asin, Title: title}})
	if err != nil || late.ID != catalogID {
		t.Fatalf("upsert again %+v, %v", late, err)
	}
}

func TestAudiobookResolvedAfterTheLibrarySyncedIsAvailableAtOnceIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s := openJobStore(t, ctx)
	asin := "B2" + newUUID()[:8]
	persistAs(t, ctx, s, absDescriptor, absSnapshot(time.Now().UTC().Truncate(time.Second), inventory.Item{SourceItemID: "li-" + asin, Title: "Late " + asin, ExternalIDs: map[string]string{"audible": asin}}))
	item, err := s.Upsert(ctx, domain.ResolvedItem{Item: domain.CatalogItem{MediaType: domain.MediaAudiobook, Provider: "audible", ProviderID: asin, Title: "Late " + asin}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = s.pool.Exec(context.Background(), `DELETE FROM catalog_items WHERE id=$1`, item.ID) })
	if got := readAvailability(t, ctx, s, item.ID); got.availability != "available" {
		t.Fatalf("availability %+v", got)
	}
}
