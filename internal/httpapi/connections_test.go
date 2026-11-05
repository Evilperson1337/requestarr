package httpapi

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/evilperson1337/requestarr/internal/auth"
	"github.com/evilperson1337/requestarr/internal/buildinfo"
	"github.com/evilperson1337/requestarr/internal/secrets"
	"github.com/evilperson1337/requestarr/internal/sources"
)

type fakeConnections struct {
	fakeReadiness
	err      error
	saved    sources.Connection
	creds    map[secrets.Name]string
	saveCall int
}

func (f *fakeConnections) ListConnections(context.Context) ([]Connection, error) { return nil, nil }
func (f *fakeConnections) SaveConnection(_ context.Context, _ string, c sources.Connection, creds map[secrets.Name]string) (Connection, error) {
	f.saveCall++
	f.saved, f.creds = c, creds
	if f.err != nil {
		return Connection{}, f.err
	}
	return Connection{Slug: c.Slug, URL: c.URL}, nil
}
func (f *fakeConnections) RemoveConnection(context.Context, string) (Connection, error) {
	return Connection{}, nil
}
func (f *fakeConnections) TestConnection(context.Context, string) (ConnectionTest, error) {
	return ConnectionTest{}, nil
}

func connectionHTTP(svc *fakeConnections, logs *bytes.Buffer, body string) *httptest.ResponseRecorder {
	logger := slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	h := NewWithBackend(logger, buildinfo.Info{}, svc, nil, nil, nil,
		&auth.Handler{Repo: csrfAuthRepo{statusAuthRepo{user: auth.User{ID: matrixUserID, Role: "admin"}}}, Secret: "test-secret", PublicURL: "http://example.test"})
	r := httptest.NewRequest(http.MethodPut, "/api/v1/admin/library-connections/plex", strings.NewReader(body))
	r.AddCookie(&http.Cookie{Name: "requestarr_session", Value: "valid"})
	r.AddCookie(&http.Cookie{Name: "requestarr_csrf", Value: "csrf-token"})
	r.Header.Set("Origin", "http://example.test")
	r.Header.Set("X-CSRF-Token", "csrf-token")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestSaveConnectionTakesTheAddressAndCredentialInOneRequest(t *testing.T) {
	svc := &fakeConnections{}
	var logs bytes.Buffer
	w := connectionHTTP(svc, &logs, `{"url":"https://plex.example.test","credentials":{"plex_token":"`+probeValue+`"}}`)
	if w.Code != 200 || svc.saveCall != 1 || svc.saved.URL != "https://plex.example.test" || svc.creds[secrets.PlexToken] != probeValue {
		t.Fatalf("status %d body %s saved %+v", w.Code, w.Body, svc.saved)
	}
	if strings.Contains(w.Body.String(), probeValue) || strings.Contains(logs.String(), probeValue) {
		t.Fatal("credential leaked into the response or logs")
	}
}

func TestSaveConnectionStillAcceptsTheAddressAlone(t *testing.T) {
	svc := &fakeConnections{}
	w := connectionHTTP(svc, &bytes.Buffer{}, `{"url":"https://plex.example.test"}`)
	if w.Code != 200 || len(svc.creds) != 0 {
		t.Fatalf("status %d creds %v", w.Code, svc.creds)
	}
}

func TestSaveConnectionAcceptsACredentialAlone(t *testing.T) {
	svc := &fakeConnections{}
	w := connectionHTTP(svc, &bytes.Buffer{}, `{"credentials":{"plex_token":"`+probeValue+`"}}`)
	if w.Code != 200 || svc.saved.URL != "" || svc.creds[secrets.PlexToken] != probeValue {
		t.Fatalf("status %d saved %+v", w.Code, svc.saved)
	}
}

func TestSaveConnectionErrorsSayWhichPartFailedWithoutLeaking(t *testing.T) {
	for _, c := range []struct {
		name string
		err  error
		want int
		code string
		part string
	}{
		{"address", sources.ErrInvalidConnection, 422, "invalid_connection", "address"},
		{"credential value", secrets.ErrInvalidValue, 422, "invalid_secret_value", "credential"},
		{"unknown credential", ErrUnknownCredential, 422, "invalid_credential", "credential"},
		{"no master key", secrets.ErrNoMasterKey, 409, "secrets_disabled", "REQUESTARR_SECRETS_KEY"},
		{"environment", ErrSetByEnvironment, 409, "set_by_environment", "environment"},
		{"storage", errors.New("pq: postgres://u:" + probeValue + "@db"), 500, "connection_update_failed", "nothing was saved"},
	} {
		t.Run(c.name, func(t *testing.T) {
			svc := &fakeConnections{err: c.err}
			var logs bytes.Buffer
			w := connectionHTTP(svc, &logs, `{"url":"https://plex.example.test","credentials":{"plex_token":"`+probeValue+`"}}`)
			assertErrorEnvelope(t, w, c.want, c.code)
			if !strings.Contains(w.Body.String(), c.part) {
				t.Errorf("message does not mention %q: %s", c.part, w.Body)
			}
			if strings.Contains(w.Body.String(), probeValue) || strings.Contains(logs.String(), probeValue) {
				t.Fatal("credential leaked")
			}
		})
	}
}

func TestSaveConnectionRefusesAnEmptyRequest(t *testing.T) {
	svc := &fakeConnections{}
	w := connectionHTTP(svc, &bytes.Buffer{}, `{"credentials":{}}`)
	assertErrorEnvelope(t, w, 422, "invalid_connection")
	if svc.saveCall != 0 {
		t.Fatal("an empty request must not reach the service")
	}
}
