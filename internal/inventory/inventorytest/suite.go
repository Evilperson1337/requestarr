// Package inventorytest is the shared contract suite for inventory.Source
// adapters (D015, D019). An adapter supplies a Harness that builds a Source
// backed by recorded fixtures for each Scenario; Run holds it to the contract:
// success yields a complete, well-formed snapshot, an empty library is a
// success distinct from failure, and every failure is an error with no items
// and no secrets in it, so a failed sync can never be mistaken for removals.
package inventorytest

import (
	"context"
	"strings"
	"testing"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/inventory"
)

// Scenario names a backend behaviour the Harness must be able to reproduce.
type Scenario string

const (
	Library       Scenario = "library"        // the recorded success fixture
	Empty         Scenario = "empty"          // libraries exist but hold nothing
	ServerError   Scenario = "server-error"   // the backend answers HTTP 5xx
	Unauthorized  Scenario = "unauthorized"   // the credential is rejected
	Malformed     Scenario = "malformed"      // a 200 whose body is not valid inventory
	Truncated     Scenario = "truncated"      // fewer items served than the backend reports
	MidwayFailure Scenario = "midway-failure" // a later page or library fails after earlier ones succeeded
)

// Harness returns a Source whose backend behaves as the scenario describes.
type Harness func(t *testing.T, s Scenario) inventory.Source

// Expectations describe the recorded success fixture and the credential.
type Expectations struct {
	LibraryItems int    // items in the Library scenario
	Secret       string // credential that must never appear in an error; optional
}

var failureScenarios = []Scenario{ServerError, Unauthorized, Malformed, Truncated, MidwayFailure}

func Run(t *testing.T, h Harness, want Expectations) {
	t.Helper()
	t.Run("descriptor", func(t *testing.T) { checkDescriptor(t, h(t, Library).Descriptor()) })
	t.Run("library", func(t *testing.T) {
		src := h(t, Library)
		snap, err := src.Snapshot(context.Background())
		if err != nil {
			t.Fatalf("success fixture failed: %v", err)
		}
		checkSnapshot(t, src.Descriptor(), snap, want.LibraryItems)
	})
	t.Run("empty library is a success, not a failure", func(t *testing.T) {
		src := h(t, Empty)
		snap, err := src.Snapshot(context.Background())
		if err != nil {
			t.Fatalf("empty library returned an error: %v", err)
		}
		checkSnapshot(t, src.Descriptor(), snap, 0)
	})
	for _, scenario := range failureScenarios {
		t.Run("failure/"+string(scenario), func(t *testing.T) {
			snap, err := h(t, scenario).Snapshot(context.Background())
			if err == nil {
				t.Fatalf("scenario %s returned no error (complete=%v items=%d)", scenario, snap.Complete, len(snap.Items))
			}
			if snap.Complete || len(snap.Items) != 0 {
				t.Errorf("failed sync leaked a snapshot: complete=%v items=%d, so it could be read as evidence or removals", snap.Complete, len(snap.Items))
			}
			if want.Secret != "" && strings.Contains(err.Error(), want.Secret) {
				t.Errorf("error leaks the credential: %v", err)
			}
		})
	}
	t.Run("cancelled context is an error", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if snap, err := h(t, Library).Snapshot(ctx); err == nil || snap.Complete {
			t.Fatalf("cancelled snapshot err=%v complete=%v", err, snap.Complete)
		}
	})
}

func checkDescriptor(t *testing.T, d inventory.Descriptor) {
	t.Helper()
	if d.ID == "" || d.Kind == "" || d.Name == "" {
		t.Errorf("descriptor needs ID, Kind and Name: %+v", d)
	}
	if len(d.MediaTypes) == 0 {
		t.Error("descriptor declares no media types")
	}
	for _, m := range d.MediaTypes {
		switch m {
		case domain.MediaMovie, domain.MediaTV, domain.MediaAlbum, domain.MediaSong, domain.MediaGame:
		default:
			t.Errorf("descriptor declares unknown media type %q", m)
		}
	}
	if len(d.IdentifierKinds) == 0 {
		t.Error("descriptor declares no identifier kinds, so nothing could match the catalog")
	}
}

func checkSnapshot(t *testing.T, d inventory.Descriptor, snap inventory.Snapshot, wantItems int) {
	t.Helper()
	if !snap.Complete {
		t.Error("successful snapshot is not marked Complete")
	}
	if snap.SourceID != d.ID {
		t.Errorf("snapshot SourceID=%q, descriptor ID=%q", snap.SourceID, d.ID)
	}
	if snap.ObservedAt.IsZero() {
		t.Error("snapshot has no ObservedAt")
	}
	if len(snap.Items) != wantItems {
		t.Fatalf("items=%d want %d", len(snap.Items), wantItems)
	}
	types := map[domain.MediaType]bool{}
	for _, m := range d.MediaTypes {
		types[m] = true
	}
	kinds := map[string]bool{}
	for _, k := range d.IdentifierKinds {
		kinds[k] = true
	}
	seen := map[string]bool{}
	for _, it := range snap.Items {
		if it.SourceID != d.ID {
			t.Errorf("item %q SourceID=%q, want %q", it.SourceItemID, it.SourceID, d.ID)
		}
		if it.SourceItemID == "" || it.Title == "" {
			t.Errorf("item needs SourceItemID and Title: %+v", it)
		}
		if seen[it.SourceItemID] {
			t.Errorf("duplicate SourceItemID %q", it.SourceItemID)
		}
		seen[it.SourceItemID] = true
		if !types[it.MediaType] {
			t.Errorf("item %q has media type %q outside the declared %v", it.SourceItemID, it.MediaType, d.MediaTypes)
		}
		for kind, value := range it.ExternalIDs {
			if !kinds[kind] || value == "" {
				t.Errorf("item %q has identifier %q=%q outside the declared kinds %v", it.SourceItemID, kind, value, d.IdentifierKinds)
			}
		}
	}
}
