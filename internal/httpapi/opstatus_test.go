package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/evilperson1337/requestarr/internal/auth"
	"github.com/evilperson1337/requestarr/internal/buildinfo"
	"github.com/evilperson1337/requestarr/internal/opstatus"
)

type opStatusReadiness struct {
	fakeReadiness
	calls int
	err   error
}

func (o *opStatusReadiness) OperationalStatus(context.Context) (opstatus.Overview, error) {
	o.calls++
	return opstatus.Overview{
		Providers:    []opstatus.Provider{{Key: "tmdb", Name: "TMDB", Configured: true}},
		Sources:      []opstatus.SourceStatus{{Slug: "jellyfin", Name: "Jellyfin", Configured: true, LastError: "library unreachable"}},
		RecentErrors: []opstatus.ErrorEntry{},
	}, o.err
}

func getOperationalStatus(ready ReadinessChecker, role string) *httptest.ResponseRecorder {
	h := New(testLogger(), buildinfo.Info{}, ready, &auth.Handler{Repo: statusAuthRepo{user: auth.User{ID: "u", Role: role}}, Secret: "test-secret"})
	r := httptest.NewRequest(http.MethodGet, "/api/v1/admin/status", nil)
	if role != "" {
		r.AddCookie(&http.Cookie{Name: "requestarr_session", Value: "valid"})
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

func TestOperationalStatusIsAdminOnly(t *testing.T) {
	for _, role := range []string{"", "viewer", "requester"} {
		ready := &opStatusReadiness{}
		rec := getOperationalStatus(ready, role)
		if role == "" {
			assertErrorEnvelope(t, rec, http.StatusUnauthorized, "authentication_required")
		} else {
			assertErrorEnvelope(t, rec, http.StatusForbidden, "forbidden")
		}
		if ready.calls != 0 {
			t.Fatalf("%q reached the status data", role)
		}
	}
	rec := getOperationalStatus(&opStatusReadiness{}, "admin")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"library unreachable"`) || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("admin status=%d cache=%q body=%s", rec.Code, rec.Header().Get("Cache-Control"), rec.Body.String())
	}
}

func TestOperationalStatusFailuresAreSafe(t *testing.T) {
	rec := getOperationalStatus(&opStatusReadiness{err: errors.New("pq: password=hunter2")}, "admin")
	assertErrorEnvelope(t, rec, http.StatusInternalServerError, "status_failed")
	if strings.Contains(rec.Body.String(), "hunter2") {
		t.Fatal("internal error leaked")
	}
}
