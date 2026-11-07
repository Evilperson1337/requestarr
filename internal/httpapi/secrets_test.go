package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/evilperson1337/requestarr/internal/auth"
	"github.com/evilperson1337/requestarr/internal/buildinfo"
	"github.com/evilperson1337/requestarr/internal/secrets"
)

const probeValue = "tmdb-token-PROBE-9f3c1"

type fakeSecrets struct {
	enabled          bool
	statuses         []secrets.Status
	setErr, clearErr error
	set              []string // "actor name"
	cleared          []string
	gotValue         string
}

func (f *fakeSecrets) Status(context.Context) ([]secrets.Status, bool, error) {
	return f.statuses, f.enabled, nil
}
func (f *fakeSecrets) Set(_ context.Context, actor string, name secrets.Name, value string) error {
	f.gotValue = value
	if f.setErr == nil {
		f.set = append(f.set, actor+" "+string(name))
	}
	return f.setErr
}
func (f *fakeSecrets) Clear(_ context.Context, actor string, name secrets.Name) error {
	if f.clearErr == nil {
		f.cleared = append(f.cleared, actor+" "+string(name))
	}
	return f.clearErr
}

func secretsHTTP(svc *fakeSecrets, logs *bytes.Buffer, role, method, path, body string) *httptest.ResponseRecorder {
	logger := slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	h := NewWithBackend(logger, buildinfo.Info{}, fakeReadiness{}, nil, nil, svc,
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

func TestSecretsStatusIsWriteOnly(t *testing.T) {
	svc := &fakeSecrets{enabled: true, statuses: []secrets.Status{{Name: secrets.TMDBReadToken, EnvVar: "REQUESTARR_TMDB_READ_TOKEN", Label: "TMDb read token", Configured: true, Source: secrets.SourceShadowed, PendingRestart: true}}}
	w := secretsHTTP(svc, &bytes.Buffer{}, "superuser", http.MethodGet, "/admin/secrets", "")
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status %d cache %q", w.Code, w.Header().Get("Cache-Control"))
	}
	var got struct {
		Enabled bool             `json:"enabled"`
		Secrets []map[string]any `json:"secrets"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || !got.Enabled || len(got.Secrets) != 1 {
		t.Fatalf("body %s err %v", w.Body, err)
	}
	s := got.Secrets[0]
	if s["name"] != "tmdb_read_token" || s["configured"] != true || s["source"] != "stored (shadowed by environment)" || s["pendingRestart"] != true || s["unreadable"] != false {
		t.Fatalf("secret %v", s)
	}
	for k := range s {
		if k == "value" || strings.Contains(strings.ToLower(k), "ciphertext") {
			t.Fatalf("response field %q leaks secret material", k)
		}
	}
}

func TestSecretsRoutesRequireAdmin(t *testing.T) {
	for _, role := range []string{"viewer", "requester"} {
		for _, c := range []struct{ method, body string }{{http.MethodGet, ""}, {http.MethodPut, `{"value":"x"}`}, {http.MethodDelete, ""}} {
			svc := &fakeSecrets{}
			w := secretsHTTP(svc, &bytes.Buffer{}, role, c.method, "/admin/secrets/tmdb_read_token", c.body)
			if c.method == http.MethodGet {
				w = secretsHTTP(svc, &bytes.Buffer{}, role, c.method, "/admin/secrets", "")
			}
			if w.Code != 403 || len(svc.set)+len(svc.cleared) != 0 {
				t.Errorf("%s %s: status %d", role, c.method, w.Code)
			}
		}
	}
}

func TestSetSecretNeverEchoesOrLogsTheValue(t *testing.T) {
	svc := &fakeSecrets{}
	var logs bytes.Buffer
	w := secretsHTTP(svc, &logs, "superuser", http.MethodPut, "/admin/secrets/tmdb_read_token", `{"value":"`+probeValue+`"}`)
	if w.Code != 204 || svc.gotValue != probeValue || svc.set[0] != matrixUserID+" tmdb_read_token" {
		t.Fatalf("status %d set %v", w.Code, svc.set)
	}
	if strings.Contains(w.Body.String(), probeValue) || strings.Contains(logs.String(), probeValue) {
		t.Fatal("value leaked into response or logs")
	}
}

func TestSetSecretErrorsAreSafeAndFixed(t *testing.T) {
	for _, c := range []struct {
		name string
		err  error
		body string
		want int
		code string
	}{
		{"no master key", secrets.ErrNoMasterKey, `{"value":"` + probeValue + `"}`, 409, "secrets_disabled"},
		{"invalid", secrets.ErrInvalidValue, `{"value":"` + probeValue + `"}`, 422, "invalid_secret_value"},
		{"unknown", secrets.ErrUnknownSecret, `{"value":"` + probeValue + `"}`, 404, "unknown_secret"},
		{"storage", errors.New("pq: connection string postgres://u:" + probeValue + "@db"), `{"value":"` + probeValue + `"}`, 500, "secret_update_failed"},
		{"saved but not applied", ErrNotApplied, `{"value":"` + probeValue + `"}`, 500, "settings_not_applied"},
		{"malformed body", nil, `{"value":"` + probeValue + `", "extra": 1}`, 400, "invalid_secret_input"},
	} {
		t.Run(c.name, func(t *testing.T) {
			svc := &fakeSecrets{setErr: c.err}
			var logs bytes.Buffer
			w := secretsHTTP(svc, &logs, "superuser", http.MethodPut, "/admin/secrets/tmdb_read_token", c.body)
			assertErrorEnvelope(t, w, c.want, c.code)
			if strings.Contains(w.Body.String(), probeValue) || strings.Contains(logs.String(), probeValue) {
				t.Fatal("value leaked")
			}
		})
	}
}

func TestClearSecret(t *testing.T) {
	svc := &fakeSecrets{}
	w := secretsHTTP(svc, &bytes.Buffer{}, "superuser", http.MethodDelete, "/admin/secrets/tmdb_read_token", "")
	if w.Code != 204 || svc.cleared[0] != matrixUserID+" tmdb_read_token" {
		t.Fatalf("status %d cleared %v", w.Code, svc.cleared)
	}
	svc = &fakeSecrets{clearErr: secrets.ErrUnknownSecret}
	assertErrorEnvelope(t, secretsHTTP(svc, &bytes.Buffer{}, "superuser", http.MethodDelete, "/admin/secrets/nope", ""), 404, "unknown_secret")
}

// PUT and DELETE on stored secrets enforce the same CSRF token and Origin
// checks as the other admin writes.
func TestSecretsWritesRequireCSRFAndSameOrigin(t *testing.T) {
	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		for _, c := range []struct{ name, origin, token, code string }{
			{"missing csrf header", "http://example.test", "", "csrf_invalid"},
			{"wrong csrf header", "http://example.test", "other", "csrf_invalid"},
			{"foreign origin", "http://evil.test", "csrf-token", "origin_rejected"},
			{"missing origin", "", "csrf-token", "origin_rejected"},
		} {
			svc := &fakeSecrets{}
			h := NewWithBackend(slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)), buildinfo.Info{}, fakeReadiness{}, nil, nil, svc,
				&auth.Handler{Repo: csrfAuthRepo{statusAuthRepo{user: auth.User{ID: matrixUserID, Role: "superuser"}}}, Secret: "test-secret", PublicURL: "http://example.test"})
			r := httptest.NewRequest(method, "/api/v1/admin/secrets/tmdb_read_token", strings.NewReader(`{"value":"x"}`))
			r.AddCookie(&http.Cookie{Name: "requestarr_session", Value: "valid"})
			r.AddCookie(&http.Cookie{Name: "requestarr_csrf", Value: "csrf-token"})
			if c.origin != "" {
				r.Header.Set("Origin", c.origin)
			}
			if c.token != "" {
				r.Header.Set("X-CSRF-Token", c.token)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != 403 || !strings.Contains(w.Body.String(), c.code) || len(svc.set)+len(svc.cleared) != 0 {
				t.Errorf("%s %s: status %d body %s writes %v %v", method, c.name, w.Code, w.Body, svc.set, svc.cleared)
			}
		}
	}
}

func TestSecretsAreWritableAtAnyTime(t *testing.T) {
	svc := &fakeSecrets{}
	if w := secretsHTTP(svc, &bytes.Buffer{}, "superuser", http.MethodPut, "/admin/secrets/tmdb_read_token", `{"value":"`+probeValue+`"}`); w.Code != 204 {
		t.Fatalf("replace: %d", w.Code)
	}
	if w := secretsHTTP(svc, &bytes.Buffer{}, "superuser", http.MethodDelete, "/admin/secrets/tmdb_read_token", ""); w.Code != 204 {
		t.Fatalf("clear: %d", w.Code)
	}
}
