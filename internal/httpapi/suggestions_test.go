package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/evilperson1337/requestarr/internal/auth"
	"github.com/evilperson1337/requestarr/internal/buildinfo"
	"github.com/evilperson1337/requestarr/internal/domain"
)

const suggestionID = "11575ac1-351c-4a1d-af13-e30628135788"

type suggestionBackend struct {
	fakeReadiness
	items    []domain.MatchSuggestion
	err      error
	calls    int
	id       string
	actor    string
	decision string
}

func (b *suggestionBackend) ListMatchSuggestions(context.Context) ([]domain.MatchSuggestion, error) {
	b.calls++
	return b.items, b.err
}

func (b *suggestionBackend) ReviewMatchSuggestion(_ context.Context, id, actor, decision string) error {
	b.calls++
	b.id, b.actor, b.decision = id, actor, decision
	return b.err
}

func suggestionRequest(backend ReadinessChecker, role, method, path string, csrf bool) *httptest.ResponseRecorder {
	h := New(testLogger(), buildinfo.Info{}, backend, &auth.Handler{Repo: csrfAuthRepo{statusAuthRepo{user: auth.User{ID: "admin-1", Role: role}}}, Secret: "test-secret", PublicURL: "http://example.test"})
	r := httptest.NewRequest(method, "/api/v1/admin/match-suggestions"+path, nil)
	if role != "" {
		r.AddCookie(&http.Cookie{Name: "requestarr_session", Value: "valid"})
	}
	r.AddCookie(&http.Cookie{Name: "requestarr_csrf", Value: "csrf-token"})
	r.Header.Set("Origin", "http://example.test")
	if csrf {
		r.Header.Set("X-CSRF-Token", "csrf-token")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestSuggestionReviewRoutesRequireAdminAndCSRF(t *testing.T) {
	for _, op := range []struct{ method, path string }{{"GET", ""}, {"POST", "/" + suggestionID + "/accept"}, {"POST", "/" + suggestionID + "/dismiss"}} {
		for _, role := range []string{"", "viewer", "requester"} {
			t.Run(op.method+op.path+role, func(t *testing.T) {
				backend := &suggestionBackend{}
				want, code := http.StatusForbidden, "forbidden"
				if role == "" {
					want, code = http.StatusUnauthorized, "authentication_required"
				}
				assertErrorEnvelope(t, suggestionRequest(backend, role, op.method, op.path, true), want, code)
				if backend.calls != 0 {
					t.Fatal("unauthorized caller reached match review store")
				}
			})
		}
	}
	for _, decision := range []string{"accept", "dismiss"} {
		backend := &suggestionBackend{}
		assertErrorEnvelope(t, suggestionRequest(backend, "admin", "POST", "/"+suggestionID+"/"+decision, false), 403, "csrf_invalid")
		if backend.calls != 0 {
			t.Fatal("missing CSRF token reached match review store")
		}
	}
}

func TestAdminCanListAcceptAndDismissSuggestions(t *testing.T) {
	backend := &suggestionBackend{items: []domain.MatchSuggestion{{ID: suggestionID, CatalogTitle: "Arrival", InventoryTitle: "ARRIVAL", Confidence: "title_year", Reason: "Review this candidate"}}}
	w := suggestionRequest(backend, "admin", "GET", "", false)
	if w.Code != 200 {
		t.Fatalf("list: %d %s", w.Code, w.Body.String())
	}
	var response struct {
		Items []domain.MatchSuggestion `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || len(response.Items) != 1 || response.Items[0].ID != suggestionID {
		t.Fatalf("list response: %+v err=%v", response, err)
	}
	for _, decision := range []string{"accept", "dismiss"} {
		w = suggestionRequest(backend, "admin", "POST", "/"+suggestionID+"/"+decision, true)
		if w.Code != 204 || backend.id != suggestionID || backend.actor != "admin-1" || backend.decision != decision {
			t.Fatalf("review %s: status=%d backend=%+v body=%s", decision, w.Code, backend, w.Body.String())
		}
	}
}

func TestSuggestionRoutesReturnSafeActionableErrors(t *testing.T) {
	for _, tc := range []struct {
		name, method, path, code string
		backend                  ReadinessChecker
		status                   int
	}{
		{"invalid ID", "POST", "/not-an-id/accept", "invalid_suggestion_id", &suggestionBackend{}, 400},
		{"unavailable", "GET", "", "match_review_unavailable", fakeReadiness{}, 503},
		{"missing", "POST", "/" + suggestionID + "/accept", "suggestion_not_found", &suggestionBackend{err: domain.ErrMatchNotFound}, 404},
		{"reviewed", "POST", "/" + suggestionID + "/dismiss", "suggestion_already_reviewed", &suggestionBackend{err: domain.ErrMatchAlreadyReviewed}, 409},
		{"stale", "POST", "/" + suggestionID + "/accept", "suggestion_stale", &suggestionBackend{err: domain.ErrMatchStale}, 409},
		{"read failure", "GET", "", "match_suggestions_failed", &suggestionBackend{err: errors.New("database secret")}, 500},
		{"write failure", "POST", "/" + suggestionID + "/accept", "match_review_failed", &suggestionBackend{err: errors.New("database secret")}, 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := suggestionRequest(tc.backend, "admin", tc.method, tc.path, true)
			assertErrorEnvelope(t, w, tc.status, tc.code)
			if strings.Contains(w.Body.String(), "database secret") {
				t.Fatal("internal error leaked")
			}
		})
	}
}
