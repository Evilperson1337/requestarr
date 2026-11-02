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
