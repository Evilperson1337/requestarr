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
	"github.com/evilperson1337/requestarr/internal/inventory/navidrome"
	"github.com/evilperson1337/requestarr/internal/jobs"
)

func navidromeServer(t *testing.T, recordingID string, failAlbum bool) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("u") != "requestarr" || r.URL.Query().Get("t") == "" || r.URL.Query().Get("s") == "" {
			http.Error(w, "bad auth", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/rest/getMusicFolders.view":
			fmt.Fprint(w, `{"subsonic-response":{"status":"ok","version":"1.16.1","musicFolders":{"musicFolder":[{"id":1,"name":"Music"}]}}}`)
		case "/rest/getAlbumList2.view":
			fmt.Fprint(w, `{"subsonic-response":{"status":"ok","version":"1.16.1","albumList2":{"album":[{"id":"album-1","name":"Single","songCount":1,"musicBrainzId":"release-group"}]}}}`)
		case "/rest/getAlbum.view":
			if failAlbum {
				http.Error(w, "library unavailable", http.StatusServiceUnavailable)
				return
			}
			fmt.Fprintf(w, `{"subsonic-response":{"status":"ok","version":"1.16.1","album":{"id":"album-1","name":"Single","songCount":1,"musicBrainzId":"release-group","song":[{"id":"song-1","title":"A Song","musicBrainzId":%q}]}}}`, recordingID)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(ts.Close)
	return ts
}

func TestNavidromeMusicRequestAvailabilityAndFailedSyncPreservation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s := openJobStore(t, ctx)
	t.Cleanup(func() {
		_, _ = s.pool.Exec(context.Background(), `DELETE FROM job_runs WHERE source_id=$1`, navidrome.SourceID)
	})
	catalogID, requestID := newMusicRequest(t, ctx, s, domain.MediaSong, domain.RequestRequested)
	runner := jobs.NewRunner(s)

	syncSource := func(ts *httptest.Server) (jobs.Run, error) {
		client, err := navidrome.NewWithHTTPClient(ts.URL, "requestarr", "test-password", ts.Client())
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

	// An initial failed sync must not create inventory or availability evidence.
	failed, err := syncSource(navidromeServer(t, catalogID, true))
	if err == nil || failed.Status != jobs.StatusFailed || failed.Error != "inventory request failed" {
		t.Fatalf("failed run=%+v err=%v", failed, err)
	}
	if got := readAvailability(t, ctx, s, catalogID); got != (availabilityState{"unknown", 0, 0, 0}) {
		t.Fatalf("failed Navidrome sync changed availability: %+v", got)
	}
	before := readRequestEvidence(t, ctx, s, catalogID, requestID)

	ts := navidromeServer(t, catalogID, false)
	for run := 1; run <= 2; run++ {
		ok, err := syncSource(ts)
		if err != nil || ok.Status != jobs.StatusSucceeded {
			t.Fatalf("successful run %d=%+v err=%v", run, ok, err)
		}
		want := requestEvidence{status: "available", version: before.version + 1, availableAt: true, auditEvents: 1, inApp: 1, apprise: 1, catalogEvents: 1}
		if got := readRequestEvidence(t, ctx, s, catalogID, requestID); got != want {
			t.Fatalf("after successful run %d: got %+v want %+v", run, got, want)
		}
	}

	before = readRequestEvidence(t, ctx, s, catalogID, requestID)
	failed, err = syncSource(navidromeServer(t, catalogID, true))
	if err == nil || failed.Status != jobs.StatusFailed {
		t.Fatalf("failed follow-up run=%+v err=%v", failed, err)
	}
	if got := readRequestEvidence(t, ctx, s, catalogID, requestID); got != before {
		t.Fatalf("failed Navidrome sync removed or changed availability: %+v -> %+v", before, got)
	}
}
