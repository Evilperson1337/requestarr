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
	"github.com/evilperson1337/requestarr/internal/checklist"
)

type fakeOnboarding struct {
	fakeReadiness
	result    checklist.Result
	err       error
	dismissed int
	igdbID    string
	igdbEnv   bool
	igdbErr   error
	saved     []string
}

func (f *fakeOnboarding) Get(context.Context) (checklist.Result, error) { return f.result, f.err }
func (f *fakeOnboarding) Dismiss(context.Context) error {
	f.dismissed++
	return f.err
}
func (f *fakeOnboarding) IGDBClientID(context.Context) (string, bool, error) {
	return f.igdbID, f.igdbEnv, f.igdbErr
}
func (f *fakeOnboarding) SetIGDBClientID(_ context.Context, id string) error {
	if f.igdbErr == nil {
		f.saved = append(f.saved, id)
		f.igdbID = id
	}
	return f.igdbErr
}

func onboardingHTTP(f *fakeOnboarding, role, method, path, body string) *httptest.ResponseRecorder {
	h := New(testLogger(), buildinfo.Info{}, f, &auth.Handler{Repo: csrfAuthRepo{statusAuthRepo{user: auth.User{ID: matrixUserID, Role: role}}}, Secret: "test-secret", PublicURL: "http://example.test"})
	r := httptest.NewRequest(method, "/api/v1"+path, strings.NewReader(body))
	if role != "" {
		r.AddCookie(&http.Cookie{Name: "requestarr_session", Value: "valid"})
	}
	r.AddCookie(&http.Cookie{Name: "requestarr_csrf", Value: "csrf-token"})
	r.Header.Set("Origin", "http://example.test")
	r.Header.Set("X-CSRF-Token", "csrf-token")
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestChecklistIsAdminOnly(t *testing.T) {
	for _, c := range []struct{ method, path string }{
		{"GET", "/admin/checklist"}, {"POST", "/admin/checklist/dismiss"},
		{"GET", "/admin/settings/igdb"}, {"PUT", "/admin/settings/igdb"},
	} {
		for _, role := range []string{"", "viewer", "requester"} {
			f := &fakeOnboarding{}
			w := onboardingHTTP(f, role, c.method, c.path, `{"clientId":"x"}`)
			want, code := 403, "forbidden"
			if role == "" {
				want, code = 401, "authentication_required"
			}
			assertErrorEnvelope(t, w, want, code)
			if f.dismissed != 0 || len(f.saved) != 0 {
				t.Errorf("%s %s as %q reached the service", c.method, c.path, role)
			}
		}
	}
}

func TestChecklistReturnsTheComputedItems(t *testing.T) {
	f := &fakeOnboarding{result: checklist.Result{Visible: true, Items: []checklist.Item{{Key: checklist.Libraries, Title: "Connect a media library", Required: true}}}}
	w := onboardingHTTP(f, "admin", "GET", "/admin/checklist", "")
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status %d cache %q", w.Code, w.Header().Get("Cache-Control"))
	}
	var got struct {
		Visible bool `json:"visible"`
		Items   []struct {
			Key      string `json:"key"`
			Required bool   `json:"required"`
		} `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || !got.Visible || len(got.Items) != 1 || got.Items[0].Key != "libraries" || !got.Items[0].Required {
		t.Fatalf("body %s err %v", w.Body, err)
	}
	f.err = errors.New("db down")
	assertErrorEnvelope(t, onboardingHTTP(f, "admin", "GET", "/admin/checklist", ""), 500, "checklist_failed")
}

func TestDismissingTheChecklistIsRemembered(t *testing.T) {
	f := &fakeOnboarding{}
	if w := onboardingHTTP(f, "admin", "POST", "/admin/checklist/dismiss", ""); w.Code != 204 || f.dismissed != 1 {
		t.Fatalf("status %d dismissed %d", w.Code, f.dismissed)
	}
	f.err = errors.New("db down")
	assertErrorEnvelope(t, onboardingHTTP(f, "admin", "POST", "/admin/checklist/dismiss", ""), 500, "checklist_failed")
}

func TestIGDBClientIDSetting(t *testing.T) {
	f := &fakeOnboarding{igdbID: "abc", igdbEnv: true}
	w := onboardingHTTP(f, "admin", "GET", "/admin/settings/igdb", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"clientId":"abc"`) || !strings.Contains(w.Body.String(), `"fromEnvironment":true`) {
		t.Fatalf("get: %d %s", w.Code, w.Body)
	}
	f = &fakeOnboarding{}
	w = onboardingHTTP(f, "admin", "PUT", "/admin/settings/igdb", `{"clientId":"twitch-id"}`)
	if w.Code != 200 || len(f.saved) != 1 || f.saved[0] != "twitch-id" || !strings.Contains(w.Body.String(), `"clientId":"twitch-id"`) {
		t.Fatalf("put: %d %s saved %v", w.Code, w.Body, f.saved)
	}
	for name, body := range map[string]string{"unknown field": `{"clientId":"a","x":1}`, "missing": `{}`, "not json": `nope`} {
		if w := onboardingHTTP(&fakeOnboarding{}, "admin", "PUT", "/admin/settings/igdb", body); w.Code != 400 {
			t.Errorf("%s: status %d", name, w.Code)
		}
	}
	assertErrorEnvelope(t, onboardingHTTP(&fakeOnboarding{igdbErr: ErrInvalidIGDBClientID}, "admin", "PUT", "/admin/settings/igdb", `{"clientId":"bad\nid"}`), 400, "invalid_client_id")
	assertErrorEnvelope(t, onboardingHTTP(&fakeOnboarding{igdbErr: ErrIGDBClientIDFromEnvironment}, "admin", "PUT", "/admin/settings/igdb", `{"clientId":"x"}`), 409, "managed_by_environment")
	assertErrorEnvelope(t, onboardingHTTP(&fakeOnboarding{igdbErr: errors.New("db down")}, "admin", "PUT", "/admin/settings/igdb", `{"clientId":"x"}`), 500, "igdb_settings_failed")
}

func TestSetupStatusNoLongerReportsTheWizard(t *testing.T) {
	w := httptest.NewRecorder()
	New(testLogger(), buildinfo.Info{}, fakeReadiness{}).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/setup/status", nil))
	if w.Code != 200 || strings.Contains(w.Body.String(), "wizard") {
		t.Fatalf("setup status %d %s", w.Code, w.Body)
	}
}
