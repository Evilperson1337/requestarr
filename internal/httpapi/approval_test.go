package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/evilperson1337/requestarr/internal/auth"
	"github.com/evilperson1337/requestarr/internal/buildinfo"
	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/requests"
	"github.com/evilperson1337/requestarr/internal/store/postgres"
)

type approvalBackend struct {
	fakeReadiness
	requests.Repository
	types    []domain.MediaType
	awaiting int
	sets     [][]domain.MediaType
}

func (b *approvalBackend) AutoApproveTypes(context.Context) ([]domain.MediaType, error) {
	return append([]domain.MediaType{}, b.types...), nil
}

func (b *approvalBackend) SetAutoApproveTypes(_ context.Context, types []domain.MediaType) error {
	b.sets = append(b.sets, types)
	for _, t := range types {
		if t == "podcast" {
			return postgres.ErrInvalidMediaType // the real store's contract
		}
	}
	b.types = types
	return nil
}

func (b *approvalBackend) AwaitingApprovalCount(context.Context) (int, error) { return b.awaiting, nil }

func approvalCall(t *testing.T, b *approvalBackend, role, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	h := New(testLogger(), buildinfo.Info{}, b, &auth.Handler{Repo: csrfAuthRepo{statusAuthRepo{user: auth.User{ID: "u-1", Role: role}}}, Secret: "test-secret", PublicURL: "http://example.test"})
	r := httptest.NewRequest(method, "/api/v1"+path, strings.NewReader(body))
	r.AddCookie(&http.Cookie{Name: "requestarr_session", Value: "valid"})
	r.AddCookie(&http.Cookie{Name: "requestarr_csrf", Value: "csrf-token"})
	r.Header.Set("X-CSRF-Token", "csrf-token")
	r.Header.Set("Origin", "http://example.test")
	r.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

func TestApprovalPolicySettingIsAdminOnly(t *testing.T) {
	for _, role := range []string{"viewer", "requester"} {
		b := &approvalBackend{}
		if rec := approvalCall(t, b, role, http.MethodGet, "/admin/settings/approvals", ""); rec.Code != http.StatusForbidden {
			t.Errorf("%s GET status=%d, want 403", role, rec.Code)
		}
		if rec := approvalCall(t, b, role, http.MethodPut, "/admin/settings/approvals", `{"autoApproveTypes":["movie"]}`); rec.Code != http.StatusForbidden || len(b.sets) != 0 {
			t.Errorf("%s PUT status=%d sets=%v, want 403 and no change", role, rec.Code, b.sets)
		}
	}
	b := &approvalBackend{}
	if rec := approvalCall(t, b, "admin", http.MethodGet, "/admin/settings/approvals", ""); rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != `{"autoApproveTypes":[]}` {
		t.Fatalf("default GET status=%d body=%s, want an empty list", rec.Code, rec.Body.String())
	}
	if rec := approvalCall(t, b, "admin", http.MethodPut, "/admin/settings/approvals", `{"autoApproveTypes":["movie","audiobook"]}`); rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != `{"autoApproveTypes":["movie","audiobook"]}` {
		t.Fatalf("PUT status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := approvalCall(t, b, "admin", http.MethodGet, "/admin/settings/approvals", ""); strings.TrimSpace(rec.Body.String()) != `{"autoApproveTypes":["movie","audiobook"]}` {
		t.Fatalf("GET after PUT body=%s", rec.Body.String())
	}
	if rec := approvalCall(t, b, "admin", http.MethodPut, "/admin/settings/approvals", `{"autoApproveTypes":[]}`); rec.Code != 200 || len(b.types) != 0 {
		t.Fatalf("clearing the policy status=%d types=%v", rec.Code, b.types)
	}
	for _, bad := range []string{`{"autoApproveTypes":["podcast"]}`, `{"autoApproveTypes":["movie","podcast"]}`, `{}`, `{"autoApproveTypes":"movie"}`, `{"autoApproveTypes":[],"x":1}`} {
		b := &approvalBackend{types: []domain.MediaType{domain.MediaGame}}
		assertErrorEnvelope(t, approvalCall(t, b, "admin", http.MethodPut, "/admin/settings/approvals", bad), http.StatusBadRequest, "invalid_media_type")
		if len(b.types) != 1 || b.types[0] != domain.MediaGame {
			t.Errorf("%s changed the policy to %v", bad, b.types)
		}
	}
}

func TestAwaitingApprovalCountIsOnlyForAdministrators(t *testing.T) {
	for _, role := range []string{"viewer", "requester"} {
		if rec := approvalCall(t, &approvalBackend{awaiting: 5}, role, http.MethodGet, "/requests/summary", ""); rec.Code != http.StatusForbidden || strings.Contains(rec.Body.String(), `"requested"`) {
			t.Errorf("%s status=%d body=%s, want 403 without the count", role, rec.Code, rec.Body.String())
		}
	}
	rec := approvalCall(t, &approvalBackend{awaiting: 5}, "admin", http.MethodGet, "/requests/summary", "")
	if rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != `{"requested":5}` || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("admin status=%d body=%s cache=%q", rec.Code, rec.Body.String(), rec.Header().Get("Cache-Control"))
	}
}
