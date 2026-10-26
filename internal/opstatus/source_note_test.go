package opstatus

import (
	"context"
	"testing"
)

func TestUnconfiguredSourceCarriesItsNote(t *testing.T) {
	svc := New(nil, []SourceRef{{Slug: "plex", Name: "Plex", Note: "stored secret unreadable"}, {Slug: "romm", Name: "RomM"}}, fakeRuns{})
	got, err := svc.Overview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	plex, romm := got.Sources[0], got.Sources[1]
	if plex.Configured || plex.Note != "stored secret unreadable" || romm.Note != "" {
		t.Fatalf("plex %+v romm %+v", plex, romm)
	}
}
