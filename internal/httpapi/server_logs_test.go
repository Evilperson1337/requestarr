package httpapi

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/evilperson1337/requestarr/internal/auth"
	"github.com/evilperson1337/requestarr/internal/buildinfo"
	"github.com/evilperson1337/requestarr/internal/logbuffer"
)

func serverLogsHTTP(t *testing.T, buf *logbuffer.Buffer, role, query string) *httptest.ResponseRecorder {
	t.Helper()
	d := testDeps(testLogger(), buildinfo.Info{}, fakeReadiness{}, nil, nil, nil, &auth.Handler{Repo: csrfAuthRepo{statusAuthRepo{user: auth.User{ID: matrixUserID, Role: role}}}, Secret: "test-secret", PublicURL: "http://example.test"})
	d.ServerLogs = buf
	r := httptest.NewRequest("GET", "/api/v1/admin/logs"+query, nil)
	if role != "" {
		r.AddCookie(&http.Cookie{Name: "requestarr_session", Value: "valid"})
	}
	w := httptest.NewRecorder()
	mustRouter(d).ServeHTTP(w, r)
	return w
}

func TestServerLogsAreSuperuserOnly(t *testing.T) {
	for _, role := range []string{"", "viewer", "requester", "admin"} {
		w := serverLogsHTTP(t, logbuffer.New(), role, "")
		want, code := 403, "forbidden"
		if role == "" {
			want, code = 401, "authentication_required"
		}
		assertErrorEnvelope(t, w, want, code)
	}
}

func TestServerLogsReturnTheBufferFiltered(t *testing.T) {
	buf := logbuffer.New()
	logger := slog.New(logbuffer.NewHandler(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}), buf))
	logger.Info("started")
	logger.Warn("sync failed", "source", "romm")
	logger.Error("broken")

	w := serverLogsHTTP(t, buf, "superuser", "?level=warn&after=2")
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status %d, cache %q: %s", w.Code, w.Header().Get("Cache-Control"), w.Body)
	}
	var got struct {
		Entries  []logbuffer.Entry `json:"entries"`
		Capacity int               `json:"capacity"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Capacity != logbuffer.Capacity || len(got.Entries) != 1 || got.Entries[0].Message != "broken" {
		t.Fatalf("got %+v", got)
	}

	for _, q := range []string{"?level=loud", "?after=-1"} {
		assertErrorEnvelope(t, serverLogsHTTP(t, buf, "superuser", q), 400, "invalid_log_query")
	}
}

func TestServerLogsWithoutABufferAreAnEmptyList(t *testing.T) {
	w := serverLogsHTTP(t, nil, "superuser", "")
	if w.Code != 200 || w.Body.String() != `{"capacity":1000,"entries":[]}`+"\n" {
		t.Fatalf("status %d: %q", w.Code, w.Body)
	}
}
