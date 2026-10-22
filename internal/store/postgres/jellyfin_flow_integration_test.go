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
	"github.com/evilperson1337/requestarr/internal/inventory/jellyfin"
	"github.com/evilperson1337/requestarr/internal/jobs"
)

func jellyfinServer(t *testing.T, tmdbID string, failing bool) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Emby-Token") != "token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if failing {
			http.Error(w, "unavailable", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"StartIndex":0,"TotalRecordCount":1,"Items":[{"Id":"jf-%s","Type":"Movie","Name":"Jellyfin flow","ProductionYear":2026,"ProviderIds":{"Tmdb":"%s"}}]}`, tmdbID, tmdbID)
	}))
	t.Cleanup(ts.Close)
	return ts
}

func jellyfinSyncThroughRunner(ctx context.Context, s *Store, runner *jobs.Runner, ts *httptest.Server) (jobs.Run, error) {
	client, err := jellyfin.NewWithHTTPClient(ts.URL, "token", jellyfinSourceID, ts.Client())
	if err != nil {
		return jobs.Run{}, err
	}
	var source inventory.Source = client
	return runner.Run(ctx, source.Descriptor().ID, jobs.TriggerSchedule, func(ctx context.Context) error {
		snap, err := source.Snapshot(ctx)
		if err != nil {
			_ = s.RecordSyncFailure(ctx, source.Descriptor(), time.Now().UTC(), "inventory request failed")
			return fmt.Errorf("inventory request failed")
		}
		return s.PersistSnapshot(ctx, source.Descriptor(), ts.URL, snap)
	})
}

func TestJellyfinSourceContractDrivesAvailabilityAndFailureChangesNothing(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s := openJobStore(t, ctx)
	t.Cleanup(func() {
		_, _ = s.pool.Exec(context.Background(), `DELETE FROM job_runs WHERE source_id=$1`, jellyfinSourceID)
	})
	catalogID, requestID, _ := newMovieWithRequest(t, ctx, s, domain.RequestRequested)
	before := readRequestEvidence(t, ctx, s, catalogID, requestID)
	runner := jobs.NewRunner(s)

	failed, err := jellyfinSyncThroughRunner(ctx, s, runner, jellyfinServer(t, catalogID, true))
	if err == nil || failed.Status != jobs.StatusFailed || failed.Error != "inventory request failed" {
		t.Fatalf("failed run=%+v err=%v", failed, err)
	}
	if got := readRequestEvidence(t, ctx, s, catalogID, requestID); got != before {
		t.Fatalf("failed Jellyfin sync changed the request: %+v -> %+v", before, got)
	}
	if got := readAvailability(t, ctx, s, catalogID); got != (availabilityState{"unknown", 0, 0, 0}) {
		t.Fatalf("failed Jellyfin sync changed availability: %+v", got)
	}

	ts := jellyfinServer(t, catalogID, false)
	for run := 1; run <= 2; run++ {
		ok, err := jellyfinSyncThroughRunner(ctx, s, runner, ts)
		if err != nil || ok.Status != jobs.StatusSucceeded {
			t.Fatalf("run %d=%+v err=%v", run, ok, err)
		}
		want := requestEvidence{status: "available", version: before.version + 1, availableAt: true, auditEvents: 1, inApp: 1, apprise: 1, catalogEvents: 1}
		if got := readRequestEvidence(t, ctx, s, catalogID, requestID); got != want {
			t.Fatalf("after run %d: got %+v want %+v", run, got, want)
		}
	}
	var source string
	if err := s.pool.QueryRow(ctx, `SELECT data->>'source' FROM request_events WHERE request_id=$1 AND event_type='request.available'`, requestID).Scan(&source); err != nil || source != "jellyfin" {
		t.Fatalf("audit event attributes the change to %q (err %v), want jellyfin", source, err)
	}
}
