package inventorytest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/inventory"
)

// fakeSource is a well-behaved adapter: it lets the suite be exercised without
// a real backend, proving the suite passes a conforming Source.
type fakeSource struct{ scenario Scenario }

const fakeID = "00000000-0000-4000-8000-000000000001"

func (fakeSource) Descriptor() inventory.Descriptor {
	return inventory.Descriptor{ID: fakeID, Kind: "fake", Name: "Fake", MediaTypes: []domain.MediaType{domain.MediaMovie, domain.MediaTV}, IdentifierKinds: []string{"tmdb"}}
}

func (f fakeSource) Snapshot(ctx context.Context) (inventory.Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return inventory.Snapshot{}, err
	}
	switch f.scenario {
	case Library:
		return inventory.Snapshot{SourceID: fakeID, Complete: true, ObservedAt: time.Now(), Items: []inventory.Item{
			{SourceID: fakeID, SourceItemID: "1", MediaType: domain.MediaMovie, Title: "Film", ExternalIDs: map[string]string{"tmdb": "603"}},
			{SourceID: fakeID, SourceItemID: "2", MediaType: domain.MediaTV, Title: "Show"},
		}}, nil
	case Empty:
		return inventory.Snapshot{SourceID: fakeID, Complete: true, ObservedAt: time.Now()}, nil
	}
	return inventory.Snapshot{}, errors.New("backend failed")
}

func TestSuitePassesAConformingSource(t *testing.T) {
	Run(t, func(_ *testing.T, s Scenario) inventory.Source { return fakeSource{scenario: s} }, Expectations{LibraryItems: 2})
}
