//go:build integration

package postgres

import (
	"testing"

	"github.com/evilperson1337/requestarr/internal/domain"
)

// A whole page is annotated with one database round trip, however many results
// it holds, and nothing outside the database is consulted.
func TestSearchLocalStatesAnnotatesAPageWithOneQueryIntegration(t *testing.T) {
	ctx, s, _ := setupDraftStore(t)
	keys := make([]domain.SearchKey, 50)
	for i := range keys {
		media := domain.MediaMovie
		if i%2 == 1 {
			media = domain.MediaTV
		}
		keys[i] = key(media, newUUID()[:8])
	}
	before := s.pool.Stat().AcquireCount()
	if _, err := s.SearchLocalStates(ctx, keys); err != nil {
		t.Fatal(err)
	}
	if n := s.pool.Stat().AcquireCount() - before; n != 1 {
		t.Fatalf("connection acquisitions = %d, want 1 query for the page", n)
	}
	if got, err := s.SearchLocalStates(ctx, nil); err != nil || len(got) != 0 {
		t.Fatalf("empty page: %v %v", got, err)
	}
}
