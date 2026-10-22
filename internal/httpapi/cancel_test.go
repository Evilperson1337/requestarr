package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/evilperson1337/requestarr/internal/auth"
	"github.com/evilperson1337/requestarr/internal/buildinfo"
	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/requests"
)

// cancelBackend scripts the repository calls the cancel/leave routes make.
type cancelBackend struct {
	fakeReadiness
	requests.Repository
	leave         [2]bool
	leaveErr      error
	subscribeErr  error
	transitionErr error
	transitioned  []requests.TransitionInput
}

func (*cancelBackend) Upsert(context.Context, domain.ResolvedItem) (domain.CatalogItem, error) {
	return domain.CatalogItem{}, nil
}

func (b *cancelBackend) Unsubscribe(context.Context, string, string) (bool, bool, error) {
	return b.leave[0], b.leave[1], b.leaveErr
}

func (b *cancelBackend) Subscribe(context.Context, string, string) (bool, error) {
	return false, b.subscribeErr
}

func (b *cancelBackend) Transition(_ context.Context, in requests.TransitionInput) (domain.Request, bool, error) {
	b.transitioned = append(b.transitioned, in)
	if b.transitionErr != nil {
		return domain.Request{}, false, b.transitionErr
	}
	return domain.Request{ID: in.RequestID, Status: in.ToStatus, Version: in.ExpectedVersion + 1}, true, nil
}

func cancelRequest(t *testing.T, backend *cancelBackend, role, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	h := New(testLogger(), buildinfo.Info{}, backend, &auth.Handler{Repo: csrfAuthRepo{statusAuthRepo{user: auth.User{ID: "user-1", Role: role}}}, Secret: "test-secret", PublicURL: "http://example.test"})
	r := httptest.NewRequest(method, "/api/v1"+path, strings.NewReader(body))
	r.AddCookie(&http.Cookie{Name: "requestarr_session", Value: "valid"})
	r.AddCookie(&http.Cookie{Name: "requestarr_csrf", Value: "csrf-token"})
	r.Header.Set("X-CSRF-Token", "csrf-token")
	r.Header.Set("Origin", "http://example.test")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

func TestLeavingReportsWhetherTheRequestWasCancelled(t *testing.T) {
	const path = "/requests/22222222-2222-2222-2222-222222222222/subscription"
	for _, tc := range []struct {
		name  string
		role  string
		leave [2]bool
		want  map[string]bool
	}{
		{"others remain", "requester", [2]bool{true, false}, map[string]bool{"subscribed": false, "changed": true, "cancelled": false}},
		{"last subscriber", "requester", [2]bool{true, true}, map[string]bool{"subscribed": false, "changed": true, "cancelled": true}},
		{"already left", "viewer", [2]bool{false, false}, map[string]bool{"subscribed": false, "changed": false, "cancelled": false}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := cancelRequest(t, &cancelBackend{leave: tc.leave}, tc.role, http.MethodDelete, path, "")
			if rec.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			var got map[string]bool
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			for k, v := range tc.want {
				if got[k] != v {
					t.Errorf("%s=%v want %v (body %s)", k, got[k], v, rec.Body.String())
				}
			}
		})
	}
}

func TestAdminCancelOutrightThroughTransitionEndpoint(t *testing.T) {
	const path = "/requests/22222222-2222-2222-2222-222222222222/transition"
	body := `{"toStatus":"cancelled","expectedVersion":3,"operationId":"33333333-3333-3333-3333-333333333333"}`
	backend := &cancelBackend{}
	rec := cancelRequest(t, backend, "admin", http.MethodPost, path, body)
	if rec.Code != http.StatusOK || len(backend.transitioned) != 1 || backend.transitioned[0].ToStatus != domain.RequestCancelled {
		t.Fatalf("admin cancel status=%d calls=%v body=%s", rec.Code, backend.transitioned, rec.Body.String())
	}
	forbidden := &cancelBackend{}
	assertErrorEnvelope(t, cancelRequest(t, forbidden, "requester", http.MethodPost, path, body), http.StatusForbidden, "forbidden")
	if len(forbidden.transitioned) != 0 {
		t.Fatal("non-admin cancel reached the repository")
	}
	assertErrorEnvelope(t, cancelRequest(t, &cancelBackend{transitionErr: requests.ErrTransitionConflict}, "admin", http.MethodPost, path, body), http.StatusConflict, "version_conflict")
	assertErrorEnvelope(t, cancelRequest(t, &cancelBackend{transitionErr: requests.ErrTransitionInvalid}, "admin", http.MethodPost, path, body), http.StatusBadRequest, "invalid_transition")
}

func TestSubscriptionRoutesReportMissingAndClosedRequests(t *testing.T) {
	const path = "/requests/22222222-2222-2222-2222-222222222222/subscription"
	assertErrorEnvelope(t, cancelRequest(t, &cancelBackend{leaveErr: requests.ErrRequestNotFound}, "requester", http.MethodDelete, path, ""), http.StatusNotFound, "request_not_found")
	assertErrorEnvelope(t, cancelRequest(t, &cancelBackend{subscribeErr: requests.ErrRequestClosed}, "viewer", http.MethodPut, path, ""), http.StatusConflict, "request_closed")
}
