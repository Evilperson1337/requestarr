package opstatus

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/jobs"
)

type fakeRuns struct {
	groups    []jobs.SourceRuns
	snapshots map[string]time.Time
	err       error
}

func (f fakeRuns) LastSnapshotTimes(context.Context) (map[string]time.Time, error) {
	return f.snapshots, f.err
}

func (f fakeRuns) RecentRunsBySource(context.Context, int) ([]jobs.SourceRuns, error) {
	return f.groups, f.err
}

func run(id, source string, status jobs.Status, started time.Time, msg string) jobs.Run {
	finished := started.Add(time.Minute)
	return jobs.Run{ID: id, SourceID: source, Status: status, StartedAt: started, FinishedAt: &finished, Error: msg}
}

func TestOverviewReportsEverySourceProviderAndRecentErrors(t *testing.T) {
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	svc := New(
		[]Provider{{Key: "tmdb", Name: "TMDB", Configured: true}, {Key: "igdb", Name: "IGDB"}},
		[]SourceRef{{Slug: "jellyfin", Name: "Jellyfin", ID: "j"}, {Slug: "plex", Name: "Plex", ID: "p"}},
		fakeRuns{snapshots: map[string]time.Time{"j": now.Add(-time.Hour)}, groups: []jobs.SourceRuns{
			{SourceID: "j", Runs: []jobs.Run{
				run("3", "j", jobs.StatusFailed, now, "library unreachable"),
				run("2", "j", jobs.StatusSucceeded, now.Add(-time.Hour), ""),
			}},
			{SourceID: "p", Runs: []jobs.Run{run("1", "p", jobs.StatusFailed, now.Add(-2*time.Hour), "bad token")}},
			{SourceID: "gone", Runs: []jobs.Run{run("0", "gone", jobs.StatusFailed, now, "removed source")}},
		}})
	got, err := svc.Overview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Providers) != 2 || !got.Providers[0].Configured || got.Providers[1].Configured {
		t.Fatalf("providers = %+v", got.Providers)
	}
	jelly, plex := got.Sources[0], got.Sources[1]
	if !jelly.Configured || jelly.LastRun == nil || jelly.LastRun.ID != "3" || jelly.LastError != "library unreachable" {
		t.Fatalf("jellyfin = %+v", jelly)
	}
	if jelly.LastSuccessAt == nil || !jelly.LastSuccessAt.Equal(now.Add(-time.Hour).Add(time.Minute)) {
		t.Fatalf("last success = %v", jelly.LastSuccessAt)
	}
	// A failed run leaves the last snapshot where the last success put it.
	if jelly.LastSnapshotAt == nil || !jelly.LastSnapshotAt.Equal(now.Add(-time.Hour)) || plex.LastSnapshotAt != nil {
		t.Fatalf("last snapshots = %v, %v", jelly.LastSnapshotAt, plex.LastSnapshotAt)
	}
	if plex.LastSuccessAt != nil || plex.LastError != "bad token" {
		t.Fatalf("plex = %+v", plex)
	}
	if len(got.RecentErrors) != 2 || got.RecentErrors[0].Run.ID != "3" || got.RecentErrors[0].Source != "Jellyfin" || got.RecentErrors[1].Source != "Plex" {
		t.Fatalf("recent errors = %+v", got.RecentErrors)
	}
}

func TestOverviewHandlesUnconfiguredSourcesAndStoreFailure(t *testing.T) {
	svc := New(nil, []SourceRef{{Slug: "romm", Name: "RomM"}}, fakeRuns{})
	got, err := svc.Overview(context.Background())
	if err != nil || len(got.Sources) != 1 || got.Sources[0].Configured || got.Sources[0].LastRun != nil || got.RecentErrors == nil || got.Providers == nil {
		t.Fatalf("unconfigured = %+v err=%v", got, err)
	}
	if _, err := New(nil, nil, fakeRuns{err: errors.New("db down")}).Overview(context.Background()); err == nil {
		t.Fatal("store failure swallowed")
	}
}
