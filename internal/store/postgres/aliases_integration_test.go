//go:build integration

package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/inventory"
)

var plexMusicDescriptor = inventory.Descriptor{ID: testPlexSourceID, Kind: "plex", Name: "Plex", MediaTypes: []domain.MediaType{domain.MediaMovie, domain.MediaTV, domain.MediaAlbum, domain.MediaSong}, IdentifierKinds: []string{"tmdb", "mbid"}}

// newMusicRequest creates a MusicBrainz catalog item of the given media type
// (identified by its release-group or recording MBID) with an open Request.
func newMusicRequest(t *testing.T, ctx context.Context, s *Store, media domain.MediaType, status domain.RequestStatus) (catalogID, requestID string) {
	t.Helper()
	catalogID = newUUID()
	if _, err := s.pool.Exec(ctx, `INSERT INTO catalog_items(id,media_type,provider,provider_id,title,artwork_kind,external_url,metadata,created_at,updated_at) VALUES($1,$2,'musicbrainz',$3,'Music integration','square','https://example.test','{}',now(),now())`, catalogID, media, catalogID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = s.pool.Exec(context.Background(), `DELETE FROM catalog_items WHERE id=$1`, catalogID) })
	user := integrationUser(t, ctx, s)
	req, _, err := s.CreateOrGet(ctx, domain.Request{CatalogItemID: catalogID, Status: domain.RequestRequested, RequestedBy: user}, domain.RequestSubscription{UserID: user, Source: domain.SubscriptionRequester})
	if err != nil {
		t.Fatal(err)
	}
	if status != domain.RequestRequested {
		if _, err := s.pool.Exec(ctx, `UPDATE requests SET status=$2 WHERE id=$1`, req.ID, status); err != nil {
			t.Fatal(err)
		}
	}
	return catalogID, req.ID
}

func musicSnapshot(media domain.MediaType, mbid string) inventory.Snapshot {
	return inventory.Snapshot{SourceID: plexMusicDescriptor.ID, Complete: true, ObservedAt: time.Now().UTC(), Items: []inventory.Item{{SourceID: plexMusicDescriptor.ID, SourceItemID: "m-" + mbid, MediaType: media, Title: "Music", ExternalIDs: map[string]string{"mbid": mbid}}}}
}

func TestAliasesLetASourcesIdentifiersMatchTheCatalogItemsCanonicalIdentity(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s := openJobStore(t, ctx)
	albumID, albumRequest := newMusicRequest(t, ctx, s, domain.MediaAlbum, domain.RequestRequested)
	releaseMBID := newUUID() // what Plex exposes for the album: a release, not the release-group
	if err := s.SaveCatalogAliases(ctx, albumID, "mbid", []string{albumID, releaseMBID}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveCatalogAliases(ctx, albumID, "mbid", []string{releaseMBID}); err != nil { // idempotent
		t.Fatal(err)
	}

	// The same value on a song must not satisfy the album: aliases are per media type.
	persistAs(t, ctx, s, plexMusicDescriptor, musicSnapshot(domain.MediaSong, releaseMBID))
	if got := readAvailability(t, ctx, s, albumID); got.availability != "unknown" {
		t.Fatalf("a song matched the album's alias: %+v", got)
	}
	// An unrelated identifier matches nothing.
	persistAs(t, ctx, s, plexMusicDescriptor, musicSnapshot(domain.MediaAlbum, newUUID()))
	if got := readAvailability(t, ctx, s, albumID); got.availability != "unknown" {
		t.Fatalf("an unknown mbid matched: %+v", got)
	}
	// The release the album's alias names does, and the Request becomes available once.
	for run := 0; run < 2; run++ {
		persistAs(t, ctx, s, plexMusicDescriptor, musicSnapshot(domain.MediaAlbum, releaseMBID))
		if got := readRequestEvidence(t, ctx, s, albumID, albumRequest); got.status != "available" || got.auditEvents != 1 || got.inApp != 1 || got.apprise != 1 {
			t.Fatalf("run %d: %+v", run+1, got)
		}
	}
}

func TestOnlyOpenMusicRequestsWithoutAliasesNeedALookup(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s := openJobStore(t, ctx)
	openAlbum, _ := newMusicRequest(t, ctx, s, domain.MediaAlbum, domain.RequestRequested)
	approvedSong, _ := newMusicRequest(t, ctx, s, domain.MediaSong, domain.RequestApproved)
	deniedAlbum, _ := newMusicRequest(t, ctx, s, domain.MediaAlbum, domain.RequestDenied)
	movieID, _, _ := newMovieWithRequest(t, ctx, s, domain.RequestRequested)

	ids := func(limit int) map[string]domain.CatalogRef {
		refs, err := s.OpenCatalogItemsNeedingAliases(ctx, "musicbrainz", limit)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]domain.CatalogRef{}
		for _, r := range refs {
			out[r.ID] = r
		}
		return out
	}
	got := ids(1000)
	if _, ok := got[openAlbum]; !ok || got[openAlbum].MediaType != domain.MediaAlbum || got[openAlbum].ProviderID != openAlbum {
		t.Fatalf("open album missing or wrong: %+v", got[openAlbum])
	}
	if _, ok := got[approvedSong]; !ok || got[approvedSong].MediaType != domain.MediaSong {
		t.Fatalf("approved song missing or wrong: %+v", got[approvedSong])
	}
	for name, id := range map[string]string{"denied request": deniedAlbum, "non-music item": movieID} {
		if _, ok := got[id]; ok {
			t.Errorf("%s was offered for lookup", name)
		}
	}
	if err := s.SaveCatalogAliases(ctx, openAlbum, "mbid", []string{openAlbum}); err != nil {
		t.Fatal(err)
	}
	if _, ok := ids(1000)[openAlbum]; ok {
		t.Fatal("an item that already has aliases was offered again")
	}
	if n := len(ids(1)); n != 1 {
		t.Fatalf("limit 1 returned %d", n)
	}
}

func TestFailedLookupsRotateToTheBackAndAreNotRetriedImmediately(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s := openJobStore(t, ctx)
	broken, _ := newMusicRequest(t, ctx, s, domain.MediaAlbum, domain.RequestRequested) // oldest: would starve the rest
	fresh, _ := newMusicRequest(t, ctx, s, domain.MediaAlbum, domain.RequestRequested)
	offered := func() map[string]bool {
		refs, err := s.OpenCatalogItemsNeedingAliases(ctx, "musicbrainz", 1000)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		for _, r := range refs {
			out[r.ID] = true
		}
		return out
	}
	if got := offered(); !got[broken] || !got[fresh] {
		t.Fatalf("both should be offered before any failure: %v", got)
	}
	if err := s.RecordAliasLookupFailure(ctx, broken); err != nil {
		t.Fatal(err)
	}
	if got := offered(); got[broken] || !got[fresh] {
		t.Fatalf("a just-failed item must wait out its backoff while others proceed: %v", got)
	}
	// Once the backoff has passed it is offered again, after items never tried.
	if _, err := s.pool.Exec(ctx, `UPDATE catalog_item_alias_attempts SET last_attempt_at=now()-interval '2 hours' WHERE catalog_item_id=$1`, broken); err != nil {
		t.Fatal(err)
	}
	refs, err := s.OpenCatalogItemsNeedingAliases(ctx, "musicbrainz", 1000)
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, r := range refs {
		if r.ID == broken || r.ID == fresh {
			order = append(order, r.ID)
		}
	}
	if len(order) != 2 || order[0] != fresh || order[1] != broken {
		t.Fatalf("order=%v, want the never-tried item first then the retried one", order)
	}
	// Success clears the item from the queue for good.
	if err := s.SaveCatalogAliases(ctx, broken, "mbid", []string{broken}); err != nil {
		t.Fatal(err)
	}
	if offered()[broken] {
		t.Fatal("an item with aliases was offered again")
	}
}
