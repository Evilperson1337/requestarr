package checklist

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/opstatus"
)

type fakeStatus struct {
	overview opstatus.Overview
	err      error
}

func (f fakeStatus) Overview(context.Context) (opstatus.Overview, error) { return f.overview, f.err }

type fakeStore struct {
	reviewed, dismissed bool
	err                 error
}

func (f *fakeStore) PolicyReviewed(context.Context) (bool, error)     { return f.reviewed, f.err }
func (f *fakeStore) ChecklistDismissed(context.Context) (bool, error) { return f.dismissed, f.err }
func (f *fakeStore) DismissChecklist(context.Context) error {
	f.dismissed = true
	return f.err
}

func synced() *time.Time { t := time.Unix(1700000000, 0); return &t }

func item(t *testing.T, r Result, key Key) Item {
	t.Helper()
	for _, i := range r.Items {
		if i.Key == key {
			return i
		}
	}
	t.Fatalf("no %s item in %+v", key, r.Items)
	return Item{}
}

func TestFreshInstallListsRequiredAndOptionalItemsAsTodo(t *testing.T) {
	r, err := New(fakeStatus{overview: opstatus.Overview{
		Providers: []opstatus.Provider{{Key: "tmdb"}, {Key: "musicbrainz", Configured: true}, {Key: "igdb"}, {Key: "apprise"}},
		Sources:   []opstatus.SourceStatus{{Slug: "jellyfin"}},
	}}, &fakeStore{}).Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !r.Visible || r.Dismissed {
		t.Fatalf("fresh install should show the checklist: %+v", r)
	}
	for _, c := range []struct {
		key      Key
		required bool
	}{{Libraries, true}, {Metadata, true}, {Notifications, false}, {People, false}} {
		got := item(t, r, c.key)
		if got.Done || got.Required != c.required || got.Title == "" {
			t.Errorf("%s = %+v, want todo required=%v", c.key, got, c.required)
		}
	}
}

func TestMusicBrainzAloneDoesNotCountAsAMetadataProvider(t *testing.T) {
	r, _ := New(fakeStatus{overview: opstatus.Overview{Providers: []opstatus.Provider{{Key: "musicbrainz", Configured: true}}}}, &fakeStore{}).Get(context.Background())
	if item(t, r, Metadata).Done {
		t.Fatal("MusicBrainz is always on, so it cannot complete the metadata item")
	}
}

func TestLibraryNeedsAConnectionThatHasSynced(t *testing.T) {
	connectedOnly := opstatus.Overview{Sources: []opstatus.SourceStatus{{Slug: "plex", Configured: true}}}
	r, _ := New(fakeStatus{overview: connectedOnly}, &fakeStore{}).Get(context.Background())
	got := item(t, r, Libraries)
	if got.Done || got.Detail == "" {
		t.Fatalf("a connected but unsynced library is not done and says why: %+v", got)
	}
	connectedOnly.Sources[0].LastSnapshotAt = synced()
	r, _ = New(fakeStatus{overview: connectedOnly}, &fakeStore{}).Get(context.Background())
	if !item(t, r, Libraries).Done {
		t.Fatal("a library with a completed snapshot is done")
	}
}

func TestChecklistHidesWhenRequiredItemsAreDone(t *testing.T) {
	r, _ := New(fakeStatus{overview: opstatus.Overview{
		Providers: []opstatus.Provider{{Key: "tmdb", Configured: true}},
		Sources:   []opstatus.SourceStatus{{Slug: "plex", Configured: true, LastSnapshotAt: synced()}},
	}}, &fakeStore{}).Get(context.Background())
	if r.Visible || r.Dismissed {
		t.Fatalf("required items done: not visible, not dismissed: %+v", r)
	}
	if item(t, r, Notifications).Done || item(t, r, People).Done {
		t.Fatal("optional items are computed on their own")
	}
}

func TestOptionalItemsReadLiveState(t *testing.T) {
	r, _ := New(fakeStatus{overview: opstatus.Overview{Providers: []opstatus.Provider{{Key: "apprise", Configured: true}}}}, &fakeStore{reviewed: true}).Get(context.Background())
	if !item(t, r, Notifications).Done || !item(t, r, People).Done {
		t.Fatalf("apprise configured and role reviewed should be done: %+v", r.Items)
	}
}

func TestDismissIsRememberedAndHidesTheChecklist(t *testing.T) {
	store := &fakeStore{}
	svc := New(fakeStatus{}, store)
	if err := svc.Dismiss(context.Background()); err != nil {
		t.Fatal(err)
	}
	r, _ := svc.Get(context.Background())
	if !r.Dismissed || r.Visible {
		t.Fatalf("dismissed checklist stays hidden: %+v", r)
	}
}

func TestErrorsAreReturnedNotHidden(t *testing.T) {
	boom := errors.New("boom")
	if _, err := New(fakeStatus{err: boom}, &fakeStore{}).Get(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("status error: %v", err)
	}
	if _, err := New(fakeStatus{}, &fakeStore{err: boom}).Get(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("store error: %v", err)
	}
}
