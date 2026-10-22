//go:build integration

package postgres

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/inventory"
	"github.com/evilperson1337/requestarr/internal/inventory/plex"
	"github.com/evilperson1337/requestarr/internal/jobs"
)

// plexServer answers like a Plex server with one movie library holding one movie
// whose TMDB id is tmdbID; when failing it answers 500 to everything.
func plexServer(t *testing.T, tmdbID string, failing bool) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failing {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/library/sections":
			fmt.Fprint(w, `{"MediaContainer":{"size":1,"Directory":[{"key":"1","type":"movie","title":"Movies"}]}}`)
		case "/library/sections/1/all":
			fmt.Fprintf(w, `{"MediaContainer":{"size":1,"totalSize":1,"offset":0,"Metadata":[{"ratingKey":"900","guid":"plex://movie/x","type":"movie","title":"Flow","year":2020,"Guid":[{"id":"tmdb://%s"}]}]}}`, tmdbID)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(ts.Close)
	return ts
}

// plexSyncThroughRunner is what the app's schedule does: one recorded run that
// snapshots Plex and persists the result.
func plexSyncThroughRunner(ctx context.Context, s *Store, runner *jobs.Runner, ts *httptest.Server) (jobs.Run, error) {
	client, err := plex.NewWithHTTPClient(ts.URL, "token", ts.Client())
	if err != nil {
		return jobs.Run{}, err
	}
	return runner.Run(ctx, plex.SourceID, jobs.TriggerSchedule, func(ctx context.Context) error {
		var src inventory.Source = client
		snap, err := src.Snapshot(ctx)
		if err != nil {
			_ = s.RecordSyncFailure(ctx, src.Descriptor(), time.Now().UTC(), "inventory request failed")
			return fmt.Errorf("inventory request failed")
		}
		return s.PersistSnapshot(ctx, src.Descriptor(), ts.URL, snap)
	})
}

func TestPlexSyncThroughTheRunnerMovesRequestsToAvailableAndFailureChangesNothing(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s := openJobStore(t, ctx)
	t.Cleanup(func() {
		_, _ = s.pool.Exec(context.Background(), `DELETE FROM job_runs WHERE source_id=$1`, plex.SourceID)
	})
	catalogID, requestID, _ := newMovieWithRequest(t, ctx, s, domain.RequestRequested)
	before := readRequestEvidence(t, ctx, s, catalogID, requestID)
	runner := jobs.NewRunner(s)

	// A failing Plex records a failed run and changes nothing.
	failed, err := plexSyncThroughRunner(ctx, s, runner, plexServer(t, catalogID, true))
	if err == nil || failed.Status != jobs.StatusFailed || failed.Error != "inventory request failed" {
		t.Fatalf("failed run=%+v err=%v", failed, err)
	}
	if got := readRequestEvidence(t, ctx, s, catalogID, requestID); got != before {
		t.Fatalf("failed Plex sync changed the request: %+v -> %+v", before, got)
	}
	if got := readAvailability(t, ctx, s, catalogID); got != (availabilityState{"unknown", 0, 0, 0}) {
		t.Fatalf("failed Plex sync changed availability: %+v", got)
	}

	// The next run succeeds: the request becomes available, exactly once even if repeated.
	ts := plexServer(t, catalogID, false)
	for run := 1; run <= 2; run++ {
		ok, err := plexSyncThroughRunner(ctx, s, runner, ts)
		if err != nil || ok.Status != jobs.StatusSucceeded {
			t.Fatalf("run %d=%+v err=%v", run, ok, err)
		}
		want := requestEvidence{status: "available", version: before.version + 1, availableAt: true, auditEvents: 1, inApp: 1, apprise: 1, catalogEvents: 1}
		if got := readRequestEvidence(t, ctx, s, catalogID, requestID); got != want {
			t.Fatalf("after run %d: got %+v want %+v", run, got, want)
		}
	}
	var source string
	if err := s.pool.QueryRow(ctx, `SELECT data->>'source' FROM request_events WHERE request_id=$1 AND event_type='request.available'`, requestID).Scan(&source); err != nil || source != "plex" {
		t.Fatalf("audit event attributes the change to %q (err %v), want plex", source, err)
	}
	runs, err := s.RecentRuns(ctx, plex.SourceID, 10)
	if err != nil || len(runs) != 3 || runs[0].Status != jobs.StatusSucceeded || runs[2].Status != jobs.StatusFailed {
		t.Fatalf("recorded runs=%+v err=%v", runs, err)
	}
}
