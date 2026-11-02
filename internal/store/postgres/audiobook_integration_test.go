//go:build integration

package postgres

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/domain"
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
