//go:build integration

package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/inventory"
)

const testPlexSourceID = "8d1b6e34-5a3f-4c0e-9a57-2f0f6a9c1d42"

var plexDescriptor = inventory.Descriptor{ID: testPlexSourceID, Kind: "plex", Name: "Plex", MediaTypes: []domain.MediaType{domain.MediaMovie, domain.MediaTV}, IdentifierKinds: []string{"tmdb", "imdb", "tvdb"}}

// withItem is a complete snapshot from d that includes the catalog item.
func withItem(d inventory.Descriptor, catalogID string, at time.Time) inventory.Snapshot {
	return inventory.Snapshot{SourceID: d.ID, Complete: true, ObservedAt: at, Items: []inventory.Item{{SourceID: d.ID, SourceItemID: "item-" + catalogID, MediaType: domain.MediaMovie, Title: "Multi-source", ExternalIDs: map[string]string{"tmdb": catalogID, "imdb": "tt0000001"}}}}
}

// without is a complete, non-empty snapshot from d that lacks the catalog item
// (empty snapshots never count as absence without opt-in).
func without(d inventory.Descriptor, catalogID string, at time.Time) inventory.Snapshot {
	return inventory.Snapshot{SourceID: d.ID, Complete: true, ObservedAt: at, Items: []inventory.Item{{SourceID: d.ID, SourceItemID: "other-" + catalogID, MediaType: domain.MediaMovie, Title: "Unrelated"}}}
}

type availabilityState struct {
	availability                 string
	evidence, available, missing int
}

func readAvailability(t *testing.T, ctx context.Context, s *Store, catalogID string) availabilityState {
	t.Helper()
	var a availabilityState
	if err := s.pool.QueryRow(ctx, `SELECT availability,
		(SELECT count(*) FROM availability_evidence WHERE catalog_item_id=$1),
		(SELECT count(*) FROM availability_events WHERE catalog_item_id=$1 AND event_type='availability.available'),
		(SELECT count(*) FROM availability_events WHERE catalog_item_id=$1 AND event_type='availability.missing')
		FROM catalog_items WHERE id=$1`, catalogID).Scan(&a.availability, &a.evidence, &a.available, &a.missing); err != nil {
		t.Fatal(err)
	}
	return a
}

func persistAs(t *testing.T, ctx context.Context, s *Store, d inventory.Descriptor, snap inventory.Snapshot) {
	t.Helper()
	if err := s.PersistSnapshot(ctx, d, "http://"+d.Kind+".example", snap); err != nil {
		t.Fatalf("%s snapshot at %v: %v", d.Kind, snap.ObservedAt, err)
	}
}

func TestAvailabilityOutlivesASourceThatDropsTheItem(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s := openJobStore(t, ctx)
	catalogID, _, _ := newMovieWithRequest(t, ctx, s, domain.RequestRequested)
	base := time.Now().UTC().Truncate(time.Second)

	persistAs(t, ctx, s, jellyfinDescriptor, withItem(jellyfinDescriptor, catalogID, base))
	persistAs(t, ctx, s, plexDescriptor, withItem(plexDescriptor, catalogID, base))
	if got := readAvailability(t, ctx, s, catalogID); got != (availabilityState{"available", 2, 1, 0}) {
		t.Fatalf("both sources present: %+v", got)
	}

	// Plex completes two syncs without it. Jellyfin still has it, so it stays available.
	persistAs(t, ctx, s, plexDescriptor, without(plexDescriptor, catalogID, base.Add(25*time.Hour)))
	persistAs(t, ctx, s, plexDescriptor, without(plexDescriptor, catalogID, base.Add(50*time.Hour)))
	if got := readAvailability(t, ctx, s, catalogID); got != (availabilityState{"available", 1, 1, 0}) {
		t.Fatalf("after Plex dropped it: %+v (want available on Jellyfin's evidence alone)", got)
	}

	// Jellyfin is now the last covering source. One absence is not enough...
	persistAs(t, ctx, s, jellyfinDescriptor, without(jellyfinDescriptor, catalogID, base.Add(60*time.Hour)))
	if got := readAvailability(t, ctx, s, catalogID); got.availability != "available" || got.missing != 0 {
		t.Fatalf("after one Jellyfin absence: %+v", got)
	}
	// ...two complete syncs a day apart are: every covering source has now synced without it.
	persistAs(t, ctx, s, jellyfinDescriptor, without(jellyfinDescriptor, catalogID, base.Add(90*time.Hour)))
	if got := readAvailability(t, ctx, s, catalogID); got != (availabilityState{"missing", 0, 1, 1}) {
		t.Fatalf("after every source dropped it: %+v", got)
	}
}

func TestFailedOrPartialSourceSyncNeverRemovesAnything(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s := openJobStore(t, ctx)
	catalogID, requestID, _ := newMovieWithRequest(t, ctx, s, domain.RequestRequested)
	base := time.Now().UTC().Truncate(time.Second)
	persistAs(t, ctx, s, plexDescriptor, withItem(plexDescriptor, catalogID, base))
	before := readAvailability(t, ctx, s, catalogID)
	beforeRequest := readRequestEvidence(t, ctx, s, catalogID, requestID)

	if err := s.RecordSyncFailure(ctx, plexDescriptor, base.Add(48*time.Hour), "inventory request failed"); err != nil {
		t.Fatal(err)
	}
	partial := without(plexDescriptor, catalogID, base.Add(72*time.Hour))
	partial.Complete = false
	if err := s.PersistSnapshot(ctx, plexDescriptor, "http://plex.example", partial); err == nil {
		t.Fatal("an incomplete snapshot was persisted")
	}
	if after := readAvailability(t, ctx, s, catalogID); after != before {
		t.Fatalf("failed/partial sync changed availability: %+v -> %+v", before, after)
	}
	if after := readRequestEvidence(t, ctx, s, catalogID, requestID); after != beforeRequest {
		t.Fatalf("failed/partial sync changed the request: %+v -> %+v", beforeRequest, after)
	}
}

func TestSecondAdapterDrivesRequestsToAvailableAndMatchesOnlyKnownIdentifiers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s := openJobStore(t, ctx)
	catalogID, requestID, _ := newMovieWithRequest(t, ctx, s, domain.RequestRequested)
	before := readRequestEvidence(t, ctx, s, catalogID, requestID)
	snap := withItem(plexDescriptor, catalogID, time.Now().UTC())
	// An imdb-only item matches no catalog provider; it is inventory, not an error.
	snap.Items = append(snap.Items, inventory.Item{SourceID: plexDescriptor.ID, SourceItemID: "imdb-only-" + catalogID, MediaType: domain.MediaMovie, Title: "No tmdb", ExternalIDs: map[string]string{"imdb": "tt9999999"}})
	for run := 0; run < 2; run++ {
		persistAs(t, ctx, s, plexDescriptor, snap)
		want := requestEvidence{status: "available", version: before.version + 1, availableAt: true, auditEvents: 1, inApp: 1, apprise: 1, catalogEvents: 1}
		if got := readRequestEvidence(t, ctx, s, catalogID, requestID); got != want {
			t.Fatalf("run %d: got %+v want %+v", run+1, got, want)
		}
	}
	var name, kind string
	if err := s.pool.QueryRow(ctx, `SELECT name,kind FROM library_sources WHERE id=$1`, testPlexSourceID).Scan(&name, &kind); err != nil || name != "Plex" || kind != "plex" {
		t.Fatalf("source row name=%q kind=%q err=%v", name, kind, err)
	}
}

func TestSnapshotItemsMustMatchTheSourcesDeclaredMediaTypes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s := openJobStore(t, ctx)
	movieOnly := plexDescriptor
	movieOnly.MediaTypes = []domain.MediaType{domain.MediaMovie}
	id := newUUID()
	snap := inventory.Snapshot{SourceID: movieOnly.ID, Complete: true, ObservedAt: time.Now().UTC(), Items: []inventory.Item{{SourceID: movieOnly.ID, SourceItemID: "tv-" + id, MediaType: domain.MediaTV, Title: "A show"}}}
	if err := s.PersistSnapshot(ctx, movieOnly, "http://plex.example", snap); err == nil {
		t.Fatal("a tv item from a movie-only source was accepted")
	}
	var n int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM inventory_items WHERE source_item_id=$1`, "tv-"+id).Scan(&n); err != nil || n != 0 {
		t.Fatalf("rejected snapshot left %d inventory rows (err %v)", n, err)
	}
}

func TestOnlyIdentifierKindsASourceDeclaresAreMatched(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s := openJobStore(t, ctx)
	catalogID, requestID, _ := newMovieWithRequest(t, ctx, s, domain.RequestRequested)
	before := readRequestEvidence(t, ctx, s, catalogID, requestID)
	// This source declares imdb only, so its tmdb value must not be trusted to match the catalog.
	imdbOnly := plexDescriptor
	imdbOnly.IdentifierKinds = []string{"imdb"}
	persistAs(t, ctx, s, imdbOnly, withItem(imdbOnly, catalogID, time.Now().UTC()))
	if got := readRequestEvidence(t, ctx, s, catalogID, requestID); got != before {
		t.Fatalf("an undeclared identifier kind matched the catalog: %+v -> %+v", before, got)
	}
	if got := readAvailability(t, ctx, s, catalogID); got != (availabilityState{"unknown", 0, 0, 0}) {
		t.Fatalf("availability changed from an undeclared identifier: %+v", got)
	}
}
