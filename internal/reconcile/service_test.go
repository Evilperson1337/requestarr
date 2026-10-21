package reconcile

import (
	"testing"
	"time"

	"github.com/example/requestarr/internal/domain"
	"github.com/example/requestarr/internal/inventory"
)

func TestSnapshotRequiresExactExternalIDForConfirmation(t *testing.T) {
	catalog := []domain.CatalogItem{{ID: "catalog-1", Provider: "tmdb", ProviderID: "42", MediaType: domain.MediaMovie, Title: "Film", Year: 2020}}
	result := MatchSnapshot(catalog, inventory.Snapshot{Complete: true, Items: []inventory.Item{{MediaType: domain.MediaMovie, Title: "Film", Year: 2020}}})
	if len(result.Confirmed) != 0 || len(result.Suggested) != 1 {
		t.Fatalf("title/year match must only suggest: %#v", result)
	}
	result = MatchSnapshot(catalog, inventory.Snapshot{Items: []inventory.Item{{ExternalIDs: map[string]string{"tmdb": "42"}}}})
	if _, ok := result.Confirmed["catalog-1"]; !ok {
		t.Fatal("exact provider ID did not confirm")
	}
}

func TestIncompleteSnapshotCannotRemoveEvidenceOrChangeLifecycle(t *testing.T) {
	// The reconciliation boundary deliberately emits positive matches only. An
	// outage/incomplete run contains no absence conclusion and request lifecycle
	// fields are outside availability reconciliation.
	prior := domain.AvailabilityEvidence{CatalogItemID: "catalog-1", SourceID: "source-1", SourceItemID: "item-1", ObservedAt: time.Now()}
	request := domain.Request{Status: domain.RequestAvailable}
	got := MatchSnapshot(nil, inventory.Snapshot{SourceID: "source-1", Complete: false})
	if len(got.Confirmed) != 0 || prior.SourceItemID != "item-1" || request.Status != domain.RequestAvailable {
		t.Fatal("incomplete snapshot must preserve evidence and lifecycle status")
	}
}
