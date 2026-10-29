package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/auth"
	"github.com/evilperson1337/requestarr/internal/buildinfo"
	"github.com/evilperson1337/requestarr/internal/jobs"
)

type fakeSyncer struct {
	err     error
	sources []string
}

func (f *fakeSyncer) SyncNow(_ context.Context, source string) (jobs.Run, error) {
	f.sources = append(f.sources, source)
	if f.err != nil {
		return jobs.Run{}, f.err
	}
	return jobs.Run{ID: "run-1", SourceID: "src-1", Trigger: jobs.TriggerManual, Status: jobs.StatusRunning, StartedAt: time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)}, nil
}

func syncNow(t *testing.T, syncer SourceSyncer, role, source string, withCSRF bool) *httptest.ResponseRecorder {
	t.Helper()
	h := NewWithSync(testLogger(), buildinfo.Info{}, fakeReadiness{}, nil, syncer, &auth.Handler{Repo: csrfAuthRepo{statusAuthRepo{user: auth.User{ID: "u", Role: role}}}, Secret: "test-secret", PublicURL: "http://example.test"})
	r := httptest.NewRequest(http.MethodPost, "/api/v1/admin/sources/"+source+"/sync", nil)
	r.AddCookie(&http.Cookie{Name: "requestarr_session", Value: "valid"})
	r.AddCookie(&http.Cookie{Name: "requestarr_csrf", Value: "csrf-token"})
	r.Header.Set("Origin", "http://example.test")
	if withCSRF {
		r.Header.Set("X-CSRF-Token", "csrf-token")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

func TestSyncNowIsAdminOnlyAndStartsAManualRun(t *testing.T) {
	denied := &fakeSyncer{}
	for _, role := range []string{"viewer", "requester"} {
		assertErrorEnvelope(t, syncNow(t, denied, role, "jellyfin", true), http.StatusForbidden, "forbidden")
	}
	if len(denied.sources) != 0 {
		t.Fatalf("non-admin reached the syncer: %v", denied.sources)
	}
	syncer := &fakeSyncer{}
	rec := syncNow(t, syncer, "admin", "jellyfin", true)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Run struct{ ID, Status, Trigger string } `json:"run"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Run.ID != "run-1" || body.Run.Status != "running" || body.Run.Trigger != "manual" || len(syncer.sources) != 1 || syncer.sources[0] != "jellyfin" {
		t.Fatalf("body=%s sources=%v", rec.Body.String(), syncer.sources)
	}
}

func TestSyncNowReportsActiveRunUnknownSourceAndMissingCSRF(t *testing.T) {
	assertErrorEnvelope(t, syncNow(t, &fakeSyncer{err: jobs.ErrAlreadyRunning}, "admin", "jellyfin", true), http.StatusConflict, "sync_in_progress")
	assertErrorEnvelope(t, syncNow(t, &fakeSyncer{err: jobs.ErrUnknownSource}, "admin", "plex", true), http.StatusNotFound, "source_not_configured")
	assertErrorEnvelope(t, syncNow(t, &fakeSyncer{err: errors.New("db down")}, "admin", "jellyfin", true), http.StatusInternalServerError, "sync_failed")
	noToken := &fakeSyncer{}
	assertErrorEnvelope(t, syncNow(t, noToken, "admin", "jellyfin", false), http.StatusForbidden, "csrf_invalid")
	if len(noToken.sources) != 0 {
		t.Fatal("request without a CSRF token reached the syncer")
	}
}

func TestSyncNowRequiresASession(t *testing.T) {
	h := New(testLogger(), buildinfo.Info{}, fakeReadiness{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/admin/sources/jellyfin/sync", nil))
	assertErrorEnvelope(t, rec, http.StatusUnauthorized, "authentication_required")
}
