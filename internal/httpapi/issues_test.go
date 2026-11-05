package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/auth"
	"github.com/evilperson1337/requestarr/internal/buildinfo"
	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/issues"
)

type fakeIssues struct {
	fakeReadiness
	err    error
	joined bool
	got    *issues.ReportInput
}

func (f *fakeIssues) ReportIssue(_ context.Context, in issues.ReportInput) (issues.ReportResult, error) {
	f.got = &in
	return issues.ReportResult{Issue: domain.Issue{ID: "i1", Title: "Arrival", Kind: in.Kind, Status: domain.IssueOpen, CreatedAt: time.Unix(1700000000, 0)}, Joined: f.joined}, f.err
}
func (f *fakeIssues) ListIssues(_ context.Context, a issues.Actor, q domain.IssueQuery) (domain.IssuePage, error) {
	if q.After == "bad" {
		return domain.IssuePage{}, issues.ErrBadCursor
	}
	return domain.IssuePage{Items: []domain.Issue{{ID: "i1", Title: a.Role + "|" + string(q.Status), Kind: domain.IssueVideo, Status: domain.IssueOpen}}}, f.err
}
func (f *fakeIssues) IssueDetail(context.Context, issues.Actor, string) (domain.IssueDetail, error) {
	return domain.IssueDetail{Issue: domain.Issue{ID: "i1", Kind: domain.IssueAudio}}, f.err
}
func (f *fakeIssues) CommentOnIssue(context.Context, issues.Actor, string, string) error {
	return f.err
}
func (f *fakeIssues) TransitionIssue(_ context.Context, _ issues.Actor, _ string, to domain.IssueStatus, _ string) (domain.Issue, error) {
	return domain.Issue{ID: "i1", Status: to, Kind: domain.IssueVideo}, f.err
}
func (f *fakeIssues) LeaveIssue(context.Context, issues.Actor, string) error    { return f.err }
func (f *fakeIssues) OpenIssueCount(context.Context, issues.Actor) (int, error) { return 3, f.err }

func issueRequest(f *fakeIssues, role, method, path, body string) *httptest.ResponseRecorder {
	logger := slog.New(slog.NewTextHandler(&strings.Builder{}, nil))
	h := NewWithBackend(logger, buildinfo.Info{}, f, nil, nil, nil,
		&auth.Handler{Repo: csrfAuthRepo{statusAuthRepo{user: auth.User{ID: matrixUserID, Role: role}}}, Secret: "test-secret", PublicURL: "http://example.test"})
	r := httptest.NewRequest(method, "/api/v1"+path, strings.NewReader(body))
	r.AddCookie(&http.Cookie{Name: "requestarr_session", Value: "valid"})
	r.AddCookie(&http.Cookie{Name: "requestarr_csrf", Value: "csrf-token"})
	r.Header.Set("Origin", "http://example.test")
	r.Header.Set("X-CSRF-Token", "csrf-token")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestReportingStatusesAndErrorsAreMapped(t *testing.T) {
	body := `{"libraryItemId":"` + matrixItemID + `","kind":"subtitles","description":"late","part":{"seasons":[2]}}`
	f := &fakeIssues{}
	if w := issueRequest(f, "viewer", "POST", "/issues", body); w.Code != 201 || !strings.Contains(w.Body.String(), `"kindLabel":"Subtitles"`) || f.got.Part.Seasons[0] != 2 || f.got.Actor.Role != "viewer" {
		t.Fatalf("new: %d %s", w.Code, w.Body.String())
	}
	f.joined = true
	if w := issueRequest(f, "requester", "POST", "/issues", body); w.Code != 200 || !strings.Contains(w.Body.String(), `"joined":true`) {
		t.Fatalf("joined: %d", w.Code)
	}
	for err, want := range map[error]int{issues.ErrInvalid: 422, issues.ErrNotFound: 404, issues.ErrRateLimited: 429, issues.ErrForbidden: 403, errors.New("db: password=hunter2"): 500} {
		w := issueRequest(&fakeIssues{err: err}, "viewer", "POST", "/issues", body)
		if w.Code != want || strings.Contains(w.Body.String(), "hunter2") {
			t.Errorf("%v -> %d %s", err, w.Code, w.Body.String())
		}
		if want == 429 && w.Header().Get("Retry-After") == "" {
			t.Error("a rate limit must say when to retry")
		}
	}
	if w := issueRequest(f, "viewer", "POST", "/issues", `{"libraryItemId":"x","kind":"video","surprise":1}`); w.Code != 400 {
		t.Errorf("unknown fields must be refused: %d", w.Code)
	}
}

func TestIssueListFiltersAndTransitionsRespectRoles(t *testing.T) {
	f := &fakeIssues{}
	if w := issueRequest(f, "viewer", "GET", "/issues?status=open", ""); w.Code != 200 || !strings.Contains(w.Body.String(), "viewer|open") {
		t.Fatalf("list: %d %s", w.Code, w.Body.String())
	}
	for name, path := range map[string]string{"status": "/issues?status=maybe", "kind": "/issues?kind=gibberish", "limit": "/issues?limit=0"} {
		if w := issueRequest(f, "viewer", "GET", path, ""); w.Code != 400 {
			t.Errorf("%s: %d", name, w.Code)
		}
	}
	if w := issueRequest(f, "viewer", "GET", "/issues?cursor=bad", ""); w.Code != 400 || !strings.Contains(w.Body.String(), "invalid_cursor") {
		t.Fatalf("cursor: %d %s", w.Code, w.Body.String())
	}
	if w := issueRequest(f, "requester", "POST", "/issues/"+matrixItemID+"/status", `{"status":"resolved"}`); w.Code != 403 {
		t.Fatalf("a requester must not resolve: %d", w.Code)
	}
	if w := issueRequest(f, "admin", "POST", "/issues/"+matrixItemID+"/status", `{"status":"closed","comment":"won't fix"}`); w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"closed"`) {
		t.Fatalf("admin close: %d %s", w.Code, w.Body.String())
	}
	if w := issueRequest(&fakeIssues{err: issues.ErrTransition}, "admin", "POST", "/issues/"+matrixItemID+"/status", `{"status":"open"}`); w.Code != 409 {
		t.Fatalf("a refused transition is a conflict: %d", w.Code)
	}
	if w := issueRequest(f, "viewer", "GET", "/issues/summary", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"open":3`) {
		t.Fatalf("summary: %d %s", w.Code, w.Body.String())
	}
}
