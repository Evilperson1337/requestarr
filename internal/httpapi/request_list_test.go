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
	"github.com/evilperson1337/requestarr/internal/requests"
)

type requestListBackend struct {
	fakeReadiness
	requests.Repository
	query requests.ListQuery
	user  string
	calls int
	page  requests.Page
	err   error
}

func (*requestListBackend) Upsert(context.Context, domain.ResolvedItem) (domain.CatalogItem, error) {
	return domain.CatalogItem{}, nil
}

func (b *requestListBackend) List(_ context.Context, user string, query requests.ListQuery) (requests.Page, error) {
	b.calls++
	b.user, b.query = user, query
	return b.page, b.err
}

func (*requestListBackend) CatalogPlatforms(context.Context, string) ([]domain.Platform, error) {
	return []domain.Platform{}, nil
}

func requestListHTTP(backend *requestListBackend, role, query string) *httptest.ResponseRecorder {
	h := New(testLogger(), buildinfo.Info{}, backend, &auth.Handler{Repo: statusAuthRepo{user: auth.User{ID: "user-1", Role: role}}, Secret: "test-secret"})
	r := httptest.NewRequest(http.MethodGet, "/api/v1/requests"+query, nil)
	if role != "" {
		r.AddCookie(&http.Cookie{Name: "requestarr_session", Value: "valid"})
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestRequestListHTTPCombinesQueryAndReturnsPage(t *testing.T) {
	for _, role := range []string{"viewer", "requester", "admin"} {
		t.Run(role, func(t *testing.T) {
			next := "opaque-next-page"
			backend := &requestListBackend{page: requests.Page{Items: []domain.Request{{ID: "request-1", Title: "Fleet", Status: domain.RequestApproved}}, NextCursor: &next}}
			w := requestListHTTP(backend, role, "?q=%20FLEET%20&status=approved&mediaType=movie&mine=true&sort=oldest&limit=2&cursor=opaque-page")
			if w.Code != 200 {
				t.Fatalf("list: %d %s", w.Code, w.Body.String())
			}
			want := requests.ListQuery{Q: "fleet", Status: domain.RequestApproved, MediaType: domain.MediaMovie, Mine: true, Sort: "oldest", Limit: 2, Cursor: "opaque-page"}
			if backend.calls != 1 || backend.query != want || backend.user != "user-1" {
				t.Fatalf("wrong query dispatch: %+v", backend)
			}
			var body struct {
				Items      []domain.Request `json:"items"`
				NextCursor *string          `json:"nextCursor"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || len(body.Items) != 1 || body.Items[0].Title != "Fleet" || body.NextCursor == nil || *body.NextCursor != next {
				t.Fatalf("page contract: %s err=%v", w.Body.String(), err)
			}
		})
	}
	backend := &requestListBackend{}
	assertErrorEnvelope(t, requestListHTTP(backend, "", "?q=fleet"), 401, "authentication_required")
	if backend.calls != 0 {
		t.Fatal("anonymous request reached repository")
	}
}

func TestRequestListCanFocusAnExactSharedRequest(t *testing.T) {
	backend := &requestListBackend{page: requests.Page{Items: []domain.Request{}}}
	w := requestListHTTP(backend, "viewer", "?requestId=22222222-2222-4222-8222-222222222222")
	if w.Code != 200 || backend.calls != 1 {
		t.Fatalf("exact Request navigation returned %d", w.Code)
	}
	encoded, _ := json.Marshal(backend.query)
	if !strings.Contains(string(encoded), `"requestId":"22222222-2222-4222-8222-222222222222"`) {
		t.Fatal("exact request filter was not preserved")
	}
}

func TestRequestListHTTPRejectsInvalidParametersBeforeQuerying(t *testing.T) {
	for _, query := range []string{
		"?mine=1", "?mine=", "?mine=true&mine=false", "?q=a&q=b", "?unknown=x",
		"?requestId=not-a-uuid", "?requestId=", "?requestId=22222222-2222-4222-8222-222222222222&requestId=22222222-2222-4222-8222-222222222222",
		"?status=missing", "?status=", "?mediaType=movies", "?mediaType=", "?sort=title", "?sort=",
		"?limit=0", "?limit=-1", "?limit=101", "?limit=1.5", "?limit=", "?limit=999999999999999999999",
		"?q=" + strings.Repeat("a", 201), "?q=%00", "?q=%FF", "?q=%ZZ", "?q=" + strings.Repeat("a", 5000),
	} {
		t.Run(query[:min(len(query), 60)], func(t *testing.T) {
			backend := &requestListBackend{}
			assertErrorEnvelope(t, requestListHTTP(backend, "viewer", query), 400, "invalid_query")
			if backend.calls != 0 {
				t.Fatal("invalid query reached repository")
			}
		})
	}
}

func TestRequestListHTTPDefaultsEmptyResultsAndSafeCursorErrors(t *testing.T) {
	backend := &requestListBackend{}
	w := requestListHTTP(backend, "viewer", "")
	if w.Code != 200 || backend.query.Sort != "newest" || backend.query.Limit != 100 || !strings.Contains(w.Body.String(), `"items":[]`) || !strings.Contains(w.Body.String(), `"nextCursor":null`) {
		t.Fatalf("defaults/empty contract: %d %s backend=%+v", w.Code, w.Body.String(), backend)
	}
	for _, query := range []string{"?cursor=", "?cursor=" + strings.Repeat("x", 513)} {
		backend = &requestListBackend{}
		assertErrorEnvelope(t, requestListHTTP(backend, "viewer", query), 400, "invalid_cursor")
		if backend.calls != 0 {
			t.Fatal("invalid cursor size reached repository")
		}
	}
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{
		{requests.ErrInvalidCursor, 400, "invalid_cursor"},
		{requests.ErrCursorExpired, 410, "cursor_expired"},
		{requests.ErrQueryTooLarge, 422, "query_too_large"},
		{errors.New("database secret"), 500, "requests_list_failed"},
	} {
		backend = &requestListBackend{err: tc.err}
		w = requestListHTTP(backend, "viewer", "?cursor=opaque-page")
		assertErrorEnvelope(t, w, tc.status, tc.code)
		if strings.Contains(w.Body.String(), "database secret") {
			t.Fatal("internal error leaked")
		}
	}
}
