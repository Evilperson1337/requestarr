package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/auth"
	"github.com/evilperson1337/requestarr/internal/buildinfo"
	"github.com/evilperson1337/requestarr/internal/jobs"
)

type jobRunsReadiness struct {
	fakeReadiness
	groups []jobs.SourceRuns
	limit  int
}

func (j *jobRunsReadiness) RecentRunsBySource(_ context.Context, perSource int) ([]jobs.SourceRuns, error) {
	j.limit = perSource
	return j.groups, nil
}

func TestJobRunsEndpointIsAdminOnlyAndListsRunsPerSource(t *testing.T) {
	finished := time.Date(2026, 10, 22, 12, 0, 5, 0, time.UTC)
	ready := &jobRunsReadiness{groups: []jobs.SourceRuns{{SourceID: "src-1", Name: "Jellyfin", Runs: []jobs.Run{
		{ID: "run-2", SourceID: "src-1", Trigger: jobs.TriggerSchedule, Status: jobs.StatusRunning, StartedAt: finished},
		{ID: "run-1", SourceID: "src-1", Trigger: jobs.TriggerSchedule, Status: jobs.StatusFailed, StartedAt: finished.Add(-time.Hour), FinishedAt: &finished, Error: "inventory request failed"},
	}}}}
	get := func(role string) *httptest.ResponseRecorder {
		h := New(testLogger(), buildinfo.Info{}, ready, &auth.Handler{Repo: statusAuthRepo{user: auth.User{ID: "u", Role: role}}, Secret: "test-secret"})
		r := httptest.NewRequest(http.MethodGet, "/api/v1/admin/job-runs", nil)
		r.AddCookie(&http.Cookie{Name: "requestarr_session", Value: "valid"})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec
	}
	assertErrorEnvelope(t, get("requester"), http.StatusForbidden, "forbidden")
	rec := get("admin")
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Sources []struct {
			SourceID string `json:"sourceId"`
			Name     string `json:"name"`
			Runs     []struct {
				ID, Status, Error string
				FinishedAt        *time.Time `json:"finishedAt"`
			} `json:"runs"`
		} `json:"sources"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Sources) != 1 || body.Sources[0].Name != "Jellyfin" || len(body.Sources[0].Runs) != 2 {
		t.Fatalf("body=%s", rec.Body.String())
	}
	if first := body.Sources[0].Runs[0]; first.ID != "run-2" || first.Status != "running" || first.FinishedAt != nil {
		t.Fatalf("running run=%+v", first)
	}
	if second := body.Sources[0].Runs[1]; second.Status != "failed" || second.Error != "inventory request failed" {
		t.Fatalf("failed run=%+v", second)
	}
	if ready.limit != 10 {
		t.Fatalf("per-source limit=%d, want 10", ready.limit)
	}
}
