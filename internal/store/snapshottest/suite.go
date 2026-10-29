// Package snapshottest is the contract for persisting a Source's snapshot: the
// rules by which presence makes an item available (D014), a Source's absence
// removes only its own evidence (D015), and incomplete or empty enumerations
// never remove anything. The PostgreSQL store and the in-memory adapter in this
// package both run it, so the rules are tested without a database and the
// database is tested for agreement with them.
package snapshottest

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/inventory"
	"github.com/evilperson1337/requestarr/internal/sources"
)

// Backend is a snapshot-persistence adapter plus the hooks the suite needs to
// seed state and observe the outcome.
type Backend interface {
	PersistSnapshot(ctx context.Context, d inventory.Descriptor, baseURL string, snap inventory.Snapshot) error
	// AddMovie creates a catalog item known to the providers under tmdbID.
	AddMovie(tb testing.TB, tmdbID string) (catalogID string)
	// OpenRequest creates an open (requested) Request for the item.
	OpenRequest(tb testing.TB, catalogID string) (requestID string)
	Availability(tb testing.TB, catalogID string) domain.Availability
	RequestStatus(tb testing.TB, requestID string) domain.RequestStatus
}

var seq atomic.Int64

var (
	jellyfin = descriptor(sources.JellyfinID)
	plex     = descriptor(sources.PlexID)
)

// descriptor is a movie Source of the registered kind with that ID.
func descriptor(id string) inventory.Descriptor {
	for _, k := range sources.All() {
		if k.ID == id {
			return inventory.Descriptor{ID: k.ID, Kind: k.Slug, Name: k.Name, MediaTypes: []domain.MediaType{domain.MediaMovie, domain.MediaTV}, IdentifierKinds: []string{"tmdb"}}
		}
	}
	panic("no Source kind with ID " + id)
}

func snapshot(d inventory.Descriptor, at time.Time, complete bool, tmdbIDs ...string) inventory.Snapshot {
	snap := inventory.Snapshot{SourceID: d.ID, Complete: complete, ObservedAt: at}
	for _, id := range tmdbIDs {
		snap.Items = append(snap.Items, inventory.Item{SourceID: d.ID, SourceItemID: d.Kind + "-" + id, MediaType: domain.MediaMovie, Title: "Contract movie", ExternalIDs: map[string]string{"tmdb": id}})
	}
	return snap
}

// Run holds a Backend to the contract. newBackend must return an empty,
// isolated backend per call.
func Run(t *testing.T, newBackend func(t *testing.T) Backend) {
	ctx := context.Background()
	day := 25 * time.Hour // longer than the one-day absence interval
	base := time.Now().UTC().Truncate(time.Second)
	persist := func(t *testing.T, b Backend, d inventory.Descriptor, snap inventory.Snapshot) {
		t.Helper()
		if err := b.PersistSnapshot(ctx, d, "https://"+d.Kind+".example", snap); err != nil {
			t.Fatalf("persist %s snapshot: %v", d.Kind, err)
		}
	}
	// A fresh tmdb id per case keeps backends that share a database apart.
	id := func(b Backend, t *testing.T) (catalogID, tmdb string) {
		t.Helper()
		tmdb = fmt.Sprintf("%d-%d", time.Now().UnixNano(), seq.Add(1))
		return b.AddMovie(t, tmdb), tmdb
	}

	t.Run("presence makes an item available and moves its open request", func(t *testing.T) {
		b := newBackend(t)
		catalog, tmdb := id(b, t)
		request := b.OpenRequest(t, catalog)
		persist(t, b, jellyfin, snapshot(jellyfin, base, true, tmdb))
		if got := b.Availability(t, catalog); got != domain.AvailabilityAvailable {
			t.Fatalf("availability = %q", got)
		}
		if got := b.RequestStatus(t, request); got != domain.RequestAvailable {
			t.Fatalf("request = %q, want available", got)
		}
	})

	t.Run("an item no Source reports is left alone", func(t *testing.T) {
		b := newBackend(t)
		catalog, _ := id(b, t)
		request := b.OpenRequest(t, catalog)
		persist(t, b, jellyfin, snapshot(jellyfin, base, true, "some-other-movie"))
		if b.Availability(t, catalog) == domain.AvailabilityAvailable || b.RequestStatus(t, request) != domain.RequestRequested {
			t.Fatal("an unmatched item changed availability or its request")
		}
	})

	t.Run("an incomplete snapshot is refused and changes nothing", func(t *testing.T) {
		b := newBackend(t)
		catalog, tmdb := id(b, t)
		if err := b.PersistSnapshot(ctx, jellyfin, "https://jellyfin.example", snapshot(jellyfin, base, false, tmdb)); err == nil {
			t.Fatal("an incomplete snapshot was accepted")
		}
		if b.Availability(t, catalog) == domain.AvailabilityAvailable {
			t.Fatal("an incomplete snapshot made an item available")
		}
	})

	t.Run("one absence does not remove an item; a second, a day later, does", func(t *testing.T) {
		b := newBackend(t)
		catalog, tmdb := id(b, t)
		other, otherTMDB := id(b, t)
		persist(t, b, jellyfin, snapshot(jellyfin, base, true, tmdb, otherTMDB))
		persist(t, b, jellyfin, snapshot(jellyfin, base.Add(time.Hour), true, otherTMDB)) // first absence
		if got := b.Availability(t, catalog); got != domain.AvailabilityAvailable {
			t.Fatalf("after one absence availability = %q", got)
		}
		persist(t, b, jellyfin, snapshot(jellyfin, base.Add(time.Hour+day), true, otherTMDB)) // second, a day on
		if got := b.Availability(t, catalog); got != domain.AvailabilityMissing {
			t.Fatalf("after two absences availability = %q, want missing", got)
		}
		if got := b.Availability(t, other); got != domain.AvailabilityAvailable {
			t.Fatalf("an item still present became %q", got)
		}
	})

	t.Run("an item that returns clears its absence streak", func(t *testing.T) {
		b := newBackend(t)
		catalog, tmdb := id(b, t)
		_, filler := id(b, t)
		persist(t, b, jellyfin, snapshot(jellyfin, base, true, tmdb, filler))
		persist(t, b, jellyfin, snapshot(jellyfin, base.Add(time.Hour), true, filler))
		persist(t, b, jellyfin, snapshot(jellyfin, base.Add(2*time.Hour), true, tmdb, filler)) // back
		persist(t, b, jellyfin, snapshot(jellyfin, base.Add(2*time.Hour+day), true, filler))   // absent once more
		if got := b.Availability(t, catalog); got != domain.AvailabilityAvailable {
			t.Fatalf("availability = %q; a returned item needs two fresh absences", got)
		}
	})

	t.Run("an empty enumeration never removes anything", func(t *testing.T) {
		b := newBackend(t)
		catalog, tmdb := id(b, t)
		persist(t, b, jellyfin, snapshot(jellyfin, base, true, tmdb))
		persist(t, b, jellyfin, snapshot(jellyfin, base.Add(time.Hour), true))
		persist(t, b, jellyfin, snapshot(jellyfin, base.Add(time.Hour+day), true))
		persist(t, b, jellyfin, snapshot(jellyfin, base.Add(2*time.Hour+2*day), true))
		if got := b.Availability(t, catalog); got != domain.AvailabilityAvailable {
			t.Fatalf("empty snapshots changed availability to %q", got)
		}
	})

	t.Run("one Source dropping an item does not hide it while another has it", func(t *testing.T) {
		b := newBackend(t)
		catalog, tmdb := id(b, t)
		_, filler := id(b, t)
		persist(t, b, jellyfin, snapshot(jellyfin, base, true, tmdb, filler))
		persist(t, b, plex, snapshot(plex, base, true, tmdb, filler))
		for _, at := range []time.Time{base.Add(time.Hour), base.Add(time.Hour + day)} { // Jellyfin drops it
			persist(t, b, jellyfin, snapshot(jellyfin, at, true, filler))
		}
		if got := b.Availability(t, catalog); got != domain.AvailabilityAvailable {
			t.Fatalf("availability = %q while Plex still has the item", got)
		}
		for _, at := range []time.Time{base.Add(2 * time.Hour), base.Add(2*time.Hour + day)} { // then Plex drops it
			persist(t, b, plex, snapshot(plex, at, true, filler))
		}
		if got := b.Availability(t, catalog); got != domain.AvailabilityMissing {
			t.Fatalf("availability = %q once every Source dropped it", got)
		}
	})
}
