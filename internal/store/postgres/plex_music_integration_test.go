//go:build integration

package postgres

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/inventory/plex"
	"github.com/evilperson1337/requestarr/internal/providers/metadata/musicbrainz"
)

// toHost sends every request to the fake server, whatever host the client asked for.
type toHost struct{ target *url.URL }

func (t toHost) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.URL.Scheme, r.URL.Host = t.target.Scheme, t.target.Host
	return http.DefaultTransport.RoundTrip(r)
}

// fakeMusicBrainz knows one release of the album's release-group and one release
// carrying the song's recording on a track.
func fakeMusicBrainz(t *testing.T, group, release, recording, track string) *musicbrainz.Aliaser {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		q := r.URL.Query()
		switch {
		case q.Get("release-group") == group:
			fmt.Fprintf(w, `{"release-count":1,"releases":[{"id":%q}]}`, release)
		case q.Get("recording") == recording:
			fmt.Fprintf(w, `{"release-count":1,"releases":[{"id":%q,"media":[{"tracks":[{"id":%q,"recording":{"id":%q}}]}]}]}`, release, track, recording)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(ts.Close)
	target, _ := url.Parse(ts.URL)
	return musicbrainz.NewAliaser(&http.Client{Transport: toHost{target}}, 0)
}

// plexMusicServer holds one album tagged with a release id and one track tagged
// with a release-track id, as Plex tags them.
func plexMusicServer(t *testing.T, albumRelease, trackID string) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/library/sections":
			fmt.Fprint(w, `{"MediaContainer":{"size":1,"Directory":[{"key":"3","type":"artist","title":"Music"}]}}`)
		case r.URL.Path == "/library/sections/3/all" && r.URL.Query().Get("type") == "9":
			fmt.Fprintf(w, `{"MediaContainer":{"size":1,"totalSize":1,"offset":0,"Metadata":[{"ratingKey":"a1","type":"album","title":"Album","Guid":[{"id":"mbid://%s"}]}]}}`, albumRelease)
		case r.URL.Path == "/library/sections/3/all" && r.URL.Query().Get("type") == "10":
			fmt.Fprintf(w, `{"MediaContainer":{"size":1,"totalSize":1,"offset":0,"Metadata":[{"ratingKey":"t1","type":"track","title":"Song","Guid":[{"id":"mbid://%s"}]}]}}`, trackID)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(ts.Close)
	return ts
}

func TestPlexMusicMakesAlbumAndSongRequestsAvailableThroughMusicBrainzAliases(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s := openJobStore(t, ctx)
	albumID, albumRequest := newMusicRequest(t, ctx, s, domain.MediaAlbum, domain.RequestRequested)
	songID, songRequest := newMusicRequest(t, ctx, s, domain.MediaSong, domain.RequestApproved)
	release, track := newUUID(), newUUID()
	aliaser := fakeMusicBrainz(t, albumID, release, songID, track)
	ts := plexMusicServer(t, release, track)
	client, err := plex.NewWithHTTPClient(ts.URL, "token", ts.Client())
	if err != nil {
		t.Fatal(err)
	}
	sync := func() {
		t.Helper()
		snap, err := client.Snapshot(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.PersistSnapshot(ctx, client.Descriptor(), ts.URL, snap); err != nil {
			t.Fatal(err)
		}
	}
	status := func(requestID string) string {
		var st string
		if err := s.pool.QueryRow(ctx, `SELECT status FROM requests WHERE id=$1`, requestID).Scan(&st); err != nil {
			t.Fatal(err)
		}
		return st
	}

	// Plex's release / release-track ids name neither request's canonical identity,
	// so before the lookup nothing matches.
	sync()
	if status(albumRequest) == "available" || status(songRequest) == "available" {
		t.Fatal("music matched without the MusicBrainz bridge")
	}

	// The refresh step: ask MusicBrainz about each open music request and keep the answers.
	pending, err := s.OpenCatalogItemsNeedingAliases(ctx, "musicbrainz", 50)
	if err != nil {
		t.Fatal(err)
	}
	refreshed := 0
	for _, ref := range pending {
		if ref.ID != albumID && ref.ID != songID {
			continue // another test's leftovers
		}
		values, err := aliaser.Aliases(ctx, ref)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.SaveCatalogAliases(ctx, ref.ID, "mbid", values); err != nil {
			t.Fatal(err)
		}
		refreshed++
	}
	if refreshed != 2 {
		t.Fatalf("refreshed %d of the 2 open music requests", refreshed)
	}

	for run := 1; run <= 2; run++ {
		sync()
		for name, c := range map[string]struct{ catalog, request string }{"album": {albumID, albumRequest}, "song": {songID, songRequest}} {
			got := readRequestEvidence(t, ctx, s, c.catalog, c.request)
			if got.status != "available" || got.auditEvents != 1 || got.inApp != 1 || got.apprise != 1 || got.catalogEvents != 1 {
				t.Fatalf("run %d, %s: %+v", run, name, got)
			}
		}
	}
	// Open requests with aliases are no longer offered for lookup.
	again, err := s.OpenCatalogItemsNeedingAliases(ctx, "musicbrainz", 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range again {
		if ref.ID == albumID || ref.ID == songID {
			t.Fatalf("%s offered for lookup again", ref.ID)
		}
	}
}
