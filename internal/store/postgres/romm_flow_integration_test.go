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
	"github.com/evilperson1337/requestarr/internal/inventory/romm"
	"github.com/evilperson1337/requestarr/internal/jobs"
)

// rommServer lists a Switch (IGDB 130) and a SNES (IGDB 19) platform and holds
// the game on each platform in platformIDs (1=SNES, 2=Switch); failing answers 500.
func rommServer(t *testing.T, igdbGameID string, platformIDs []int, failing bool) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failing {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/platforms":
			fmt.Fprint(w, `[{"id":1,"igdb_id":19},{"id":2,"igdb_id":130}]`)
		case "/api/roms":
			items := ""
			for i, p := range platformIDs {
				if i > 0 {
					items += ","
				}
				items += fmt.Sprintf(`{"id":%d,"name":"Flow Game","igdb_id":%s,"platform_id":%d,"missing_from_fs":false}`, 100+p, igdbGameID, p)
			}
			fmt.Fprintf(w, `{"items":[%s],"total":%d,"limit":200,"offset":0}`, items, len(platformIDs))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(ts.Close)
	return ts
}

func rommSync(ctx context.Context, s *Store, runner *jobs.Runner, ts *httptest.Server) (jobs.Run, error) {
	client, err := romm.NewWithHTTPClient(ts.URL, "token", ts.Client())
	if err != nil {
		return jobs.Run{}, err
	}
	return runner.Run(ctx, romm.SourceID, jobs.TriggerSchedule, func(ctx context.Context) error {
		var src inventory.Source = client
		snap, err := src.Snapshot(ctx)
		if err != nil {
			_ = s.RecordSyncFailure(ctx, src.Descriptor(), time.Now().UTC(), "inventory request failed")
			return fmt.Errorf("inventory request failed")
		}
		return s.PersistSnapshot(ctx, src.Descriptor(), ts.URL, snap)
	})
}

func TestRommDrivesAGamesPlatformSpecificRequestThroughTheRunner(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s := openJobStore(t, ctx)
	t.Cleanup(func() {
		_, _ = s.pool.Exec(context.Background(), `DELETE FROM job_runs WHERE source_id=$1`, romm.SourceID)
	})
	// The Request accepts the Switch only; the game's IGDB id is the catalog item's provider id.
	catalogID, requestID := newGameRequest(t, ctx, s, domain.PlatformSelection{PlatformKeys: []string{platformSwitch}})
	// RomM reports IGDB ids as numbers, so give the catalog item a numeric one.
	igdbID := fmt.Sprint(time.Now().UnixNano() % 1_000_000_000)
	if _, err := s.pool.Exec(ctx, `UPDATE catalog_items SET provider_id=$2 WHERE id=$1`, catalogID, igdbID); err != nil {
		t.Fatal(err)
	}
	runner := jobs.NewRunner(s)

	// A failing RomM changes nothing.
	if run, err := rommSync(ctx, s, runner, rommServer(t, igdbID, nil, true)); err == nil || run.Status != jobs.StatusFailed {
		t.Fatalf("failed run=%+v err=%v", run, err)
	}
	if got := readAvailability(t, ctx, s, catalogID); got != (availabilityState{"unknown", 0, 0, 0}) {
		t.Fatalf("failed RomM sync changed availability: %+v", got)
	}

	// RomM has the game on SNES only: the game is available, this Request is not.
	if run, err := rommSync(ctx, s, runner, rommServer(t, igdbID, []int{1}, false)); err != nil || run.Status != jobs.StatusSucceeded {
		t.Fatalf("run=%+v err=%v", run, err)
	}
	if got := readAvailability(t, ctx, s, catalogID); got.availability != "available" {
		t.Fatalf("game on SNES: %+v", got)
	}
	if status := requestStatus(t, ctx, s, requestID); status != "requested" {
		t.Fatalf("Switch-only request satisfied by the SNES ROM: %s", status)
	}

	// The Switch ROM appears: available, exactly once across repeated runs.
	ts := rommServer(t, igdbID, []int{1, 2}, false)
	for run := 1; run <= 2; run++ {
		if r, err := rommSync(ctx, s, runner, ts); err != nil || r.Status != jobs.StatusSucceeded {
			t.Fatalf("run %d=%+v err=%v", run, r, err)
		}
		if got := readRequestEvidence(t, ctx, s, catalogID, requestID); got.status != "available" || got.auditEvents != 1 || got.inApp != 1 || got.apprise != 1 {
			t.Fatalf("run %d: %+v", run, got)
		}
	}
	var source string
	if err := s.pool.QueryRow(ctx, `SELECT data->>'source' FROM request_events WHERE request_id=$1 AND event_type='request.available'`, requestID).Scan(&source); err != nil || source != "romm" {
		t.Fatalf("audit source=%q err=%v", source, err)
	}
}
