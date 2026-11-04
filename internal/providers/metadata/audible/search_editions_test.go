package audible

import (
	"context"
	"testing"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/providers/metadata"
)

// Two editions of one title are two ASINs, so two rows (D031).
func TestSearchKeepsEditionsAsSeparateRows(t *testing.T) {
	r, _ := searcher(t, domain.RegionUS, fixture(t))
	page, err := r.Search(context.Background(), metadata.SearchQuery{Text: "hail mary", Page: 1})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, res := range page.Results {
		if res.Title == "Hail Mary" {
			ids = append(ids, res.ProviderID)
		}
	}
	if len(ids) != 2 || ids[0] == ids[1] {
		t.Fatalf("editions = %v", ids)
	}
}
