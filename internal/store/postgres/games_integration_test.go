//go:build integration

package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/inventory"
)

const testRommSourceID = "c2f7b0a4-6d19-4e55-8b3e-5a1f9d7e3c68"

var rommDescriptor = inventory.Descriptor{ID: testRommSourceID, Kind: "romm", Name: "RomM", MediaTypes: []domain.MediaType{domain.MediaGame}, IdentifierKinds: []string{"igdb"}}

const (
	platformSNES   = "igdb:19"
	platformSwitch = "igdb:130"
	platformPS5    = "igdb:167"
)

func newGameRequest(t *testing.T, ctx context.Context, s *Store, sel domain.PlatformSelection) (catalogID, requestID string) {
	t.Helper()
	catalogID = newUUID()
	if _, err := s.pool.Exec(ctx, `INSERT INTO catalog_items(id,media_type,provider,provider_id,title,artwork_kind,external_url,metadata,created_at,updated_at) VALUES($1,'game','igdb',$2,'Game integration','poster','https://example.test','{}',now(),now())`, catalogID, catalogID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = s.pool.Exec(context.Background(), `DELETE FROM catalog_items WHERE id=$1`, catalogID) })
	user := integrationUser(t, ctx, s)
	req, _, err := s.CreateOrGet(ctx, domain.Request{CatalogItemID: catalogID, Status: domain.RequestRequested, RequestedBy: user, Platforms: sel}, domain.RequestSubscription{UserID: user, Source: domain.SubscriptionRequester})
	if err != nil {
		t.Fatal(err)
	}
	return catalogID, req.ID
}

// gameOn is a complete RomM snapshot with the game on each given platform (a
// ROM per platform, as RomM stores them). "" is a platform RomM cannot map to IGDB.
func gameOn(igdbID string, at time.Time, platforms ...string) inventory.Snapshot {
	snap := inventory.Snapshot{SourceID: rommDescriptor.ID, Complete: true, ObservedAt: at}
	for _, p := range platforms {
		snap.Items = append(snap.Items, inventory.Item{SourceID: rommDescriptor.ID, SourceItemID: "rom-" + igdbID + "-" + p, MediaType: domain.MediaGame, Title: "Game", PlatformKey: p, ExternalIDs: map[string]string{"igdb": igdbID}})
	}
	return snap
}

// otherGame is a non-empty snapshot that lacks the game (empty ones never count as absence).
func otherGame(at time.Time) inventory.Snapshot {
	return inventory.Snapshot{SourceID: rommDescriptor.ID, Complete: true, ObservedAt: at, Items: []inventory.Item{{SourceID: rommDescriptor.ID, SourceItemID: "rom-other", MediaType: domain.MediaGame, Title: "Other", PlatformKey: platformSNES, ExternalIDs: map[string]string{"igdb": "no-such-game"}}}}
}

func requestStatus(t *testing.T, ctx context.Context, s *Store, requestID string) string {
	t.Helper()
	var status string
	if err := s.pool.QueryRow(ctx, `SELECT status FROM requests WHERE id=$1`, requestID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	return status
}

func TestGameRequestWithSeveralPlatformsIsSatisfiedWhenAnySelectedOnePresent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s := openJobStore(t, ctx)
	catalogID, requestID := newGameRequest(t, ctx, s, domain.PlatformSelection{PlatformKeys: []string{platformPS5, platformSwitch}})
	now := time.Now().UTC()

	// The game exists, but only on a platform nobody asked for: available as a game, not for this Request.
	persistAs(t, ctx, s, rommDescriptor, gameOn(catalogID, now, platformSNES))
	if got := readAvailability(t, ctx, s, catalogID); got.availability != "available" {
		t.Fatalf("game on any platform is available: %+v", got)
	}
	if status := requestStatus(t, ctx, s, requestID); status != "requested" {
		t.Fatalf("request satisfied by an unselected platform: %s", status)
	}
	// One selected platform arriving is enough (OR), and it happens once.
	for run := 0; run < 2; run++ {
		persistAs(t, ctx, s, rommDescriptor, gameOn(catalogID, now.Add(time.Duration(run+1)*time.Minute), platformSNES, platformSwitch))
		if got := readRequestEvidence(t, ctx, s, catalogID, requestID); got.status != "available" || got.auditEvents != 1 || got.inApp != 1 || got.apprise != 1 {
			t.Fatalf("run %d: %+v", run+1, got)
		}
	}
}

func TestAnyPlatformRequestIsSatisfiedByAnyPlatformIncludingOneRommCannotIdentify(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s := openJobStore(t, ctx)
	catalogID, requestID := newGameRequest(t, ctx, s, domain.PlatformSelection{Any: true})
	persistAs(t, ctx, s, rommDescriptor, gameOn(catalogID, time.Now().UTC(), ""))
	if status := requestStatus(t, ctx, s, requestID); status != "available" {
		t.Fatalf("Any request not satisfied by a game on an unidentified platform: %s", status)
	}
}

func TestUnidentifiedPlatformNeverSatisfiesAnExplicitSelection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s := openJobStore(t, ctx)
	catalogID, requestID := newGameRequest(t, ctx, s, domain.PlatformSelection{PlatformKeys: []string{platformSwitch}})
	persistAs(t, ctx, s, rommDescriptor, gameOn(catalogID, time.Now().UTC(), ""))
	if status := requestStatus(t, ctx, s, requestID); status != "requested" {
		t.Fatalf("unknown platform evidence satisfied an explicit selection: %s", status)
	}
}

func TestPlatformEvidenceIsDroppedPerPlatformAndGameGoesMissingWhenAllAreGone(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s := openJobStore(t, ctx)
	catalogID, _ := newGameRequest(t, ctx, s, domain.PlatformSelection{Any: true})
	base := time.Now().UTC().Truncate(time.Second)
	persistAs(t, ctx, s, rommDescriptor, gameOn(catalogID, base, platformSNES, platformSwitch))
	if got := readAvailability(t, ctx, s, catalogID); got != (availabilityState{"available", 2, 1, 0}) {
		t.Fatalf("two platforms present: %+v", got)
	}
	// The Switch ROM disappears across two complete syncs; the SNES one remains.
	persistAs(t, ctx, s, rommDescriptor, gameOn(catalogID, base.Add(25*time.Hour), platformSNES))
	persistAs(t, ctx, s, rommDescriptor, gameOn(catalogID, base.Add(50*time.Hour), platformSNES))
	if got := readAvailability(t, ctx, s, catalogID); got != (availabilityState{"available", 1, 1, 0}) {
		t.Fatalf("after the Switch ROM left: %+v", got)
	}
	// Then the SNES ROM goes too: no platform holds the game any more.
	persistAs(t, ctx, s, rommDescriptor, otherGame(base.Add(60*time.Hour)))
	persistAs(t, ctx, s, rommDescriptor, otherGame(base.Add(90*time.Hour)))
	if got := readAvailability(t, ctx, s, catalogID); got != (availabilityState{"missing", 0, 1, 1}) {
		t.Fatalf("after every platform left: %+v", got)
	}
}
