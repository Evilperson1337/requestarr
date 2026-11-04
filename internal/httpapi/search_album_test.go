package httpapi

import (
	"context"
	"testing"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/providers/metadata"
)

// The "include singles & EPs" switch travels from the query string to the provider.
func TestSearchPassesTheSinglesSwitchToTheProvider(t *testing.T) {
	var seen []metadata.SearchQuery
	svc := metadata.NewSearchService(map[string]metadata.Searcher{"album": albumSearcherFunc(func(q metadata.SearchQuery) { seen = append(seen, q) })})
	h := searchHandler(t, "viewer", svc)
	for _, path := range []string{"/search?q=dark&type=album", "/search?q=dark&type=album&singles=true"} {
		if w := searchGet(h, path, true); w.Code != 200 {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
	}
	if len(seen) != 2 || seen[0].IncludeSingles || !seen[1].IncludeSingles {
		t.Fatalf("provider saw %+v", seen)
	}
}

type albumSearcherFunc func(metadata.SearchQuery)

func (f albumSearcherFunc) Search(_ context.Context, q metadata.SearchQuery) (domain.SearchPage, error) {
	f(q)
	return domain.SearchPage{}, nil
}
