//go:build integration

package postgres

import (
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/inventory"
	"github.com/evilperson1337/requestarr/internal/requests"
)

func mbKey(media domain.MediaType, id string) domain.SearchKey {
	return domain.SearchKey{MediaType: media, Provider: "musicbrainz", ProviderID: id}
}

// uuidLower is a valid-looking MusicBrainz id unique to one test run.
func uuidLower() string { return newUUID() }

func persistMusic(t *testing.T, s *Store, kind string, kinds []string, items ...inventory.Item) {
	t.Helper()
	d := inventory.Descriptor{ID: newUUID(), Kind: kind, Name: kind, MediaTypes: []domain.MediaType{domain.MediaAlbum, domain.MediaSong}, IdentifierKinds: kinds}
	for i := range items {
		items[i].SourceID = d.ID
	}
	snap := inventory.Snapshot{SourceID: d.ID, Complete: true, ObservedAt: time.Now().UTC(), Items: items}
	if err := s.PersistSnapshot(t.Context(), d, "http://"+kind+".example.test", snap); err != nil {
		t.Fatal(err)
	}
}

// Plex tags an album with a release id, not the release group the search row
// names. The catalog item's aliases are the bridge, so once an album has been
// opened a Plex edition shows as held; media types stay separate namespaces.
func TestSearchLocalStatesMatchesMusicBrainzThroughAliasesIntegration(t *testing.T) {
	ctx, s, _ := integrationStore(t)
	group, release, track, recording := uuidLower(), uuidLower(), uuidLower(), uuidLower()
	album, err := s.Upsert(ctx, domain.ResolvedItem{
		Item:    domain.CatalogItem{MediaType: domain.MediaAlbum, Provider: "musicbrainz", ProviderID: group, Title: "Album"},
		Aliases: []domain.Alias{{Kind: "mbid", Value: release}},
	})
	if err != nil {
		t.Fatal(err)
	}
	song, err := s.Upsert(ctx, domain.ResolvedItem{Item: domain.CatalogItem{MediaType: domain.MediaSong, Provider: "musicbrainz", ProviderID: recording, Title: "Song"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveCatalogAliases(ctx, song.ID, "mbid", []string{recording, track}); err != nil {
		t.Fatal(err)
	}
	persistMusic(t, s, "plex", []string{"mbid"},
		inventory.Item{SourceItemID: "a-" + release, MediaType: domain.MediaAlbum, Title: "Album", ExternalIDs: map[string]string{"mbid": release}},
		inventory.Item{SourceItemID: "t-" + track, MediaType: domain.MediaSong, Title: "Song", ExternalIDs: map[string]string{"mbid": track}},
	)
	got, err := s.SearchLocalStates(ctx, []domain.SearchKey{mbKey(domain.MediaAlbum, group), mbKey(domain.MediaSong, recording)})
	if err != nil {
		t.Fatal(err)
	}
	if l := got[mbKey(domain.MediaAlbum, group)]; l.CatalogID != album.ID || l.Availability != domain.AvailabilityAvailable {
		t.Fatalf("album held under a release id: %+v", l)
	}
	if l := got[mbKey(domain.MediaSong, recording)]; l.CatalogID != song.ID || l.Availability != domain.AvailabilityAvailable {
		t.Fatalf("song held under a release-track id: %+v", l)
	}

	// The release id of an album does not make a song held, nor the reverse.
	crossSong, err := s.Upsert(ctx, domain.ResolvedItem{Item: domain.CatalogItem{MediaType: domain.MediaSong, Provider: "musicbrainz", ProviderID: uuidLower(), Title: "Other song"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveCatalogAliases(ctx, crossSong.ID, "mbid", []string{release}); err != nil {
		t.Fatal(err)
	}
	got, err = s.SearchLocalStates(ctx, []domain.SearchKey{mbKey(domain.MediaSong, crossSong.ProviderID)})
	if err != nil {
		t.Fatal(err)
	}
	if l := got[mbKey(domain.MediaSong, crossSong.ProviderID)]; l.Availability == domain.AvailabilityAvailable {
		t.Fatalf("an album's id must not hold a song: %+v", l)
	}
}

// An item nobody has opened has no catalog row and so no aliases: a Plex
// edition under a release id cannot be tied to the search row, and the answer
// is "unknown" (no local state), never a claim that the library lacks it.
func TestSearchLocalStatesNeverOpenedAlbumUnderReleaseIDIsUnknownIntegration(t *testing.T) {
	ctx, s, _ := integrationStore(t)
	group, release := uuidLower(), uuidLower()
	persistMusic(t, s, "plex", []string{"mbid"}, inventory.Item{SourceItemID: "a-" + release, MediaType: domain.MediaAlbum, Title: "Album", ExternalIDs: map[string]string{"mbid": release}})
	got, err := s.SearchLocalStates(ctx, []domain.SearchKey{mbKey(domain.MediaAlbum, group)})
	if err != nil {
		t.Fatal(err)
	}
	if l, ok := got[mbKey(domain.MediaAlbum, group)]; ok {
		t.Fatalf("no catalog row means unknown, got %+v", l)
	}
}

// A library that stores the canonical id itself (Navidrome keeps the
// MusicBrainz id it was given; Plex can too) needs no alias and no catalog row.
func TestSearchLocalStatesMatchesCanonicalMusicBrainzIDDirectlyIntegration(t *testing.T) {
	ctx, s, _ := integrationStore(t)
	group, recording, plexGroup := uuidLower(), uuidLower(), uuidLower()
	persistMusic(t, s, "navidrome", []string{"musicbrainz"},
		inventory.Item{SourceItemID: "a", MediaType: domain.MediaAlbum, Title: "Album", ExternalIDs: map[string]string{"musicbrainz": group}},
		inventory.Item{SourceItemID: "s", MediaType: domain.MediaSong, Title: "Song", ExternalIDs: map[string]string{"musicbrainz": recording}},
	)
	persistMusic(t, s, "plex", []string{"mbid"}, inventory.Item{SourceItemID: "pa", MediaType: domain.MediaAlbum, Title: "Album", ExternalIDs: map[string]string{"mbid": plexGroup}})
	keys := []domain.SearchKey{mbKey(domain.MediaAlbum, group), mbKey(domain.MediaSong, recording), mbKey(domain.MediaAlbum, plexGroup), mbKey(domain.MediaSong, group)}
	got, err := s.SearchLocalStates(ctx, keys)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range keys[:3] {
		if l := got[k]; l.Availability != domain.AvailabilityAvailable {
			t.Errorf("%v should be held: %+v", k, l)
		}
	}
	if _, ok := got[keys[3]]; ok {
		t.Error("an album id must not hold a song")
	}
}

// Requested shows for music the same way it does for video: it follows the
// catalog row of the search key's own media type.
func TestSearchLocalStatesReportsMusicRequestsIntegration(t *testing.T) {
	ctx, s, user := integrationStore(t)
	id := uuidLower()
	song, err := s.Upsert(ctx, domain.ResolvedItem{Item: domain.CatalogItem{MediaType: domain.MediaSong, Provider: "musicbrainz", ProviderID: id, Title: "Song"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := requests.New(s).Create(ctx, requests.CreateInput{Actor: requests.Actor{ID: user, Role: "requester"}, CatalogID: song.ID}); err != nil {
		t.Fatal(err)
	}
	got, err := s.SearchLocalStates(ctx, []domain.SearchKey{mbKey(domain.MediaSong, id), mbKey(domain.MediaAlbum, id)})
	if err != nil {
		t.Fatal(err)
	}
	if l := got[mbKey(domain.MediaSong, id)]; l.RequestStatus != domain.RequestRequested {
		t.Fatalf("song = %+v", l)
	}
	if _, ok := got[mbKey(domain.MediaAlbum, id)]; ok {
		t.Fatal("an album key must not see the song's request")
	}
}
