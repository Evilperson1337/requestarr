package musicbrainz

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

const (
	album  = "11111111-1111-4111-8111-111111111111"
	track  = "22222222-2222-4222-8222-222222222222"
	pressA = "33333333-3333-4333-8333-333333333333"
)

func fixture(t *testing.T, bodies map[string]string) (*Resolver, *[]string) {
	t.Helper()
	var seen []string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Path)
		body, ok := bodies[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(s.Close)
	client := s.Client()
	client.Transport = rewriteTransport{base: s.URL, next: http.DefaultTransport}
	return New(client), &seen
}

// An album preview needs the same facts a movie gets: who made it, what kind of
// release it is, when, its genres and rating, and cover art.
func TestAlbumResolvesWithDetailsAndCoverArt(t *testing.T) {
	r, _ := fixture(t, map[string]string{"/ws/2/release-group/" + album: `{"id":"` + album + `","title":"Kid A","primary-type":"Album","secondary-types":["Live"],
"first-release-date":"2000-10-02","artist-credit":[{"name":"Radiohead"}],"genres":[{"name":"electronic","count":9},{"name":"art rock","count":4}],"rating":{"value":4.5,"votes-count":22}}`})
	u, _ := url.Parse("https://musicbrainz.org/release-group/" + album)
	got, err := r.Resolve(context.Background(), u)
	if err != nil {
		t.Fatal(err)
	}
	if got.Item.ArtworkURL != "https://coverartarchive.org/release-group/"+album+"/front-500" {
		t.Fatalf("artwork = %q", got.Item.ArtworkURL)
	}
	m := got.Music
	if m == nil || strings.Join(m.Artists, ",") != "Radiohead" || m.PrimaryType != "Album" || strings.Join(m.SecondaryTypes, ",") != "Live" || m.ReleaseDate != "2000-10-02" || strings.Join(m.Genres, ",") != "electronic,art rock" {
		t.Fatalf("music = %+v", m)
	}
	if m.Score == nil || *m.Score != 4.5 || m.RatingCount != 22 {
		t.Fatalf("rating = %v / %d", m.Score, m.RatingCount)
	}
}

// A song takes its length, the releases it appears on, and the cover of the first of them.
func TestSongResolvesWithLengthAppearancesAndTheFirstReleasesCover(t *testing.T) {
	r, _ := fixture(t, map[string]string{"/ws/2/recording/" + track: `{"id":"` + track + `","title":"Everything in Its Right Place","length":251000,
"first-release-date":"2000-10-02","artist-credit":[{"name":"Radiohead"}],"releases":[{"id":"` + pressA + `","title":"Kid A"},{"id":"x","title":"Amnesiac"}]}`})
	u, _ := url.Parse("https://musicbrainz.org/recording/" + track)
	got, err := r.Resolve(context.Background(), u)
	if err != nil {
		t.Fatal(err)
	}
	if got.Item.ArtworkURL != "https://coverartarchive.org/release/"+pressA+"/front-500" {
		t.Fatalf("artwork = %q", got.Item.ArtworkURL)
	}
	m := got.Music
	if m == nil || m.LengthSeconds != 251 || strings.Join(m.AppearsOn, "|") != "Kid A|Amnesiac" {
		t.Fatalf("music = %+v", m)
	}
}

// A release link resolves to its release-group and then reads that group's
// details, since the release itself carries no genres or rating for the album.
func TestReleaseLinkReadsTheGroupsDetails(t *testing.T) {
	r, seen := fixture(t, map[string]string{
		"/ws/2/release/" + pressA:      `{"id":"` + pressA + `","title":"Deluxe","artist-credit":[{"name":"Radiohead"}],"release-group":{"id":"` + album + `","title":"Kid A"}}`,
		"/ws/2/release-group/" + album: `{"id":"` + album + `","title":"Kid A","primary-type":"Album","artist-credit":[{"name":"Radiohead"}],"genres":[{"name":"electronic"}]}`,
	})
	u, _ := url.Parse("https://musicbrainz.org/release/" + pressA)
	got, err := r.Resolve(context.Background(), u)
	if err != nil {
		t.Fatal(err)
	}
	if got.Item.ProviderID != album || got.Music == nil || len(got.Music.Genres) != 1 || len(*seen) != 2 {
		t.Fatalf("item = %+v music = %+v requests = %v", got.Item, got.Music, *seen)
	}
}

func TestDetailsAreBoundedAndCleaned(t *testing.T) {
	genres := ""
	for i := 0; i < 30; i++ {
		genres += `{"name":"g"},`
	}
	r, _ := fixture(t, map[string]string{"/ws/2/release-group/" + album: `{"id":"` + album + `","title":"T","genres":[` + genres + `{"name":"\u0007bell"}],"rating":{"value":99,"votes-count":3}}`})
	u, _ := url.Parse("https://musicbrainz.org/release-group/" + album)
	got, err := r.Resolve(context.Background(), u)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Music.Genres) > 10 || got.Music.Score != nil {
		t.Fatalf("genres = %d, score = %v: genres cap at 10 and a score off the 0-5 scale is dropped", len(got.Music.Genres), got.Music.Score)
	}
}

// The bug report: MusicBrainz answered 503 now and then, and a release link
// (two calls) failed with the generic metadata error. One blip must not fail it.
func TestReleaseLinkSurvivesAnOccasional503(t *testing.T) {
	calls := map[string]int{}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls[r.URL.Path]++
		if calls[r.URL.Path] == 1 { // every endpoint fails once
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/release/") {
			_, _ = w.Write([]byte(`{"id":"` + pressA + `","title":"Deluxe","release-group":{"id":"` + album + `","title":"Kid A"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"` + album + `","title":"Kid A","artist-credit":[{"name":"Radiohead"}]}`))
	}))
	defer s.Close()
	client := s.Client()
	client.Transport = rewriteTransport{base: s.URL, next: http.DefaultTransport}
	u, _ := url.Parse("https://musicbrainz.org/release/" + pressA)
	got, err := New(client).Resolve(context.Background(), u)
	if err != nil || got.Item.ProviderID != album {
		t.Fatalf("err = %v, item = %+v", err, got.Item)
	}
}

const pressB = "44444444-4444-4444-8444-444444444444"
const trackOne = "55555555-5555-4555-8555-555555555555"
const trackTwo = "66666666-6666-4666-8666-666666666666"

func groupWithReleases() string {
	return `{"id":"` + album + `","title":"Kid A","primary-type":"Album","first-release-date":"2000-10-02","artist-credit":[{"name":"Radiohead"}],
"releases":[{"id":"` + pressB + `","title":"Kid A","status":"Official","date":"2001-01-01"},{"id":"` + pressA + `","title":"Kid A","status":"Official","date":"2000-10-02"}]}`
}

// An album carries its tracklist so a Request can pick tracks; discs flatten into one running position.
func TestAlbumCarriesTheEarliestOfficialReleasesTracklist(t *testing.T) {
	r, seen := fixture(t, map[string]string{
		"/ws/2/release-group/" + album: groupWithReleases(),
		"/ws/2/release/" + pressA:      `{"media":[{"tracks":[{"id":"` + trackOne + `","title":"Everything in Its Right Place","length":251000}]},{"tracks":[{"id":"` + trackTwo + `","title":"Kid A","length":284000},{"id":"bad","title":"Skipped: not an MBID"}]}]}`,
	})
	u, _ := url.Parse("https://musicbrainz.org/release-group/" + album)
	got, err := r.Resolve(context.Background(), u)
	if err != nil {
		t.Fatal(err)
	}
	tracks := got.Music.Tracks
	if len(tracks) != 2 || tracks[0].ID != trackOne || tracks[0].Position != 1 || tracks[0].LengthSeconds != 251 || tracks[1].Position != 2 || tracks[1].Title != "Kid A" {
		t.Fatalf("tracks = %+v", tracks)
	}
	if strings.Join(*seen, ",") != "/ws/2/release-group/"+album+",/ws/2/release/"+pressA {
		t.Fatalf("calls = %v", *seen)
	}
}

// A failed tracklist never blocks the album: it is requestable whole.
func TestAlbumWithoutATracklistStillResolves(t *testing.T) {
	r, _ := fixture(t, map[string]string{"/ws/2/release-group/" + album: groupWithReleases()})
	u, _ := url.Parse("https://musicbrainz.org/release-group/" + album)
	got, err := r.Resolve(context.Background(), u)
	if err != nil || got.Music == nil || len(got.Music.Tracks) != 0 {
		t.Fatalf("got %+v err %v", got.Music, err)
	}
}
