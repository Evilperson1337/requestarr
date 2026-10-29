package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/evilperson1337/requestarr/internal/auth"
	"github.com/evilperson1337/requestarr/internal/store/postgres"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/evilperson1337/requestarr/internal/buildinfo"
)

// assertErrorEnvelope checks the one JSON error shape every API failure uses
// and returns the request ID it carries.
func assertErrorEnvelope(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int, wantCode string) string {
	t.Helper()
	if rec.Code != wantStatus {
		t.Fatalf("status=%d want %d body=%s", rec.Code, wantStatus, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type=%q want application/json body=%s", ct, rec.Body.String())
	}
	var body struct {
		Error struct {
			Code      string `json:"code"`
			Message   string `json:"message"`
			RequestID string `json:"requestId"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not the JSON error shape: %v body=%s", err, rec.Body.String())
	}
	if body.Error.Code != wantCode {
		t.Errorf("code=%q want %q", body.Error.Code, wantCode)
	}
	if body.Error.Message == "" {
		t.Error("message is empty")
	}
	if body.Error.RequestID == "" {
		t.Fatal("requestId is empty")
	}
	if got := rec.Header().Get("X-Request-Id"); got != body.Error.RequestID {
		t.Errorf("X-Request-Id=%q, body requestId=%q", got, body.Error.RequestID)
	}
	return body.Error.RequestID
}

func TestUnauthenticatedAPIRequestUsesErrorEnvelope(t *testing.T) {
	handler := New(testLogger(), buildinfo.Info{}, fakeReadiness{})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/requests", nil))
	assertErrorEnvelope(t, rec, http.StatusUnauthorized, "authentication_required")
}

func TestRequestIDAppearsInRequestLog(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	handler := New(logger, buildinfo.Info{}, fakeReadiness{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/requests", nil)
	req.Header.Set("X-Request-Id", "client-chosen")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	id := assertErrorEnvelope(t, rec, http.StatusUnauthorized, "authentication_required")
	if id == "client-chosen" {
		t.Fatal("client-supplied request ID was trusted")
	}
	var entry map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(logs.Bytes()), &entry); err != nil {
		t.Fatalf("log line is not JSON: %v: %s", err, logs.String())
	}
	if entry["request_id"] != id {
		t.Fatalf("logged request_id=%v want %q", entry["request_id"], id)
	}
	if entry["status"] != float64(http.StatusUnauthorized) {
		t.Fatalf("logged status=%v want 401", entry["status"])
	}
}

func TestCommonFailureStatusesUseErrorEnvelope(t *testing.T) {
	const target = "11111111-1111-1111-1111-111111111111"
	admin := auth.User{ID: "admin-1", Role: "admin"}
	viewer := auth.User{ID: "viewer-1", Role: "viewer"}
	for _, tc := range []struct {
		name       string
		user       auth.User
		ready      ReadinessChecker
		method     string
		path, body string
		csrf       string
		origin     string
		status     int
		code       string
	}{
		{"bad role body", admin, &roleChangeReadiness{}, "PUT", "/admin/users/" + target + "/role", `{"role":"owner"}`, "csrf-token", "http://example.test", 400, "invalid_role"},
		{"bad user id", admin, &roleChangeReadiness{}, "PUT", "/admin/users/nope/role", `{"role":"viewer"}`, "csrf-token", "http://example.test", 400, "invalid_user_id"},
		{"non-admin", viewer, &roleChangeReadiness{}, "PUT", "/admin/users/" + target + "/role", `{"role":"viewer"}`, "csrf-token", "http://example.test", 403, "forbidden"},
		{"bad csrf", admin, &roleChangeReadiness{}, "PUT", "/admin/users/" + target + "/role", `{"role":"viewer"}`, "wrong", "http://example.test", 403, "csrf_invalid"},
		{"bad origin", admin, &roleChangeReadiness{}, "PUT", "/admin/users/" + target + "/role", `{"role":"viewer"}`, "csrf-token", "http://evil.test", 403, "origin_rejected"},
		{"missing user", admin, &roleChangeReadiness{err: postgres.ErrAdminUserNotFound}, "PUT", "/admin/users/" + target + "/role", `{"role":"viewer"}`, "csrf-token", "http://example.test", 404, "user_not_found"},
		{"last admin", admin, &roleChangeReadiness{err: postgres.ErrLastAdmin}, "PUT", "/admin/users/" + target + "/role", `{"role":"viewer"}`, "csrf-token", "http://example.test", 409, "last_admin"},
		{"unexpected failure", admin, &roleChangeReadiness{err: errors.New("boom")}, "PUT", "/admin/users/" + target + "/role", `{"role":"viewer"}`, "csrf-token", "http://example.test", 500, "role_change_failed"},
		{"unknown route", admin, fakeReadiness{}, "GET", "/no-such-route", "", "", "", 404, "not_found"},
		{"wrong method", admin, fakeReadiness{}, "DELETE", "/version", "", "csrf-token", "http://example.test", 405, "method_not_allowed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := New(testLogger(), buildinfo.Info{}, tc.ready, &auth.Handler{Repo: csrfAuthRepo{statusAuthRepo{user: tc.user}}, Secret: "test-secret", PublicURL: "http://example.test"})
			r := httptest.NewRequest(tc.method, "/api/v1"+tc.path, strings.NewReader(tc.body))
			r.AddCookie(&http.Cookie{Name: "requestarr_session", Value: "valid"})
			r.AddCookie(&http.Cookie{Name: "requestarr_csrf", Value: "csrf-token"})
			if tc.csrf != "" {
				r.Header.Set("X-CSRF-Token", tc.csrf)
			}
			if tc.origin != "" {
				r.Header.Set("Origin", tc.origin)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, r)
			assertErrorEnvelope(t, rec, tc.status, tc.code)
		})
	}
}
