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
	"github.com/evilperson1337/requestarr/internal/setup"
)

type fakeSetup struct {
	calls int
	actor setup.Actor
	err   error
}

func (f *fakeSetup) Load(_ context.Context, actor setup.Actor) (setup.Draft, error) {
	f.calls++
	f.actor = actor
	return setup.Draft{Revision: 1, CurrentStep: setup.StepGeneral, Inactive: true}, f.err
}
func (f *fakeSetup) Save(ctx context.Context, actor setup.Actor, input setup.SaveInput) (setup.Draft, error) {
	return f.Load(ctx, actor)
}
func (f *fakeSetup) Validate(ctx context.Context, actor setup.Actor, revision int64, target setup.Target) (setup.Draft, error) {
	return f.Load(ctx, actor)
}

func setupHTTP(service *fakeSetup, role, method, path, body string, csrf bool) *httptest.ResponseRecorder {
	h := NewWithSetup(testLogger(), buildinfo.Info{}, fakeReadiness{}, nil, nil, service,
		&auth.Handler{Repo: csrfAuthRepo{statusAuthRepo{user: auth.User{ID: matrixUserID, Role: role}}}, Secret: "test-secret", PublicURL: "http://example.test"})
	r := httptest.NewRequest(method, "/api/v1/admin/setup/"+path, strings.NewReader(body))
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

func TestSetupDraftEndpointsRequireAdminAndProtectSecrets(t *testing.T) {
	for _, endpoint := range []struct{ method, path, body string }{
		{"GET", "draft", ""},
		{"PUT", "draft", `{"expectedRevision":1,"currentStep":"general","settings":{"general":{"instanceName":"","timezone":"","publicUrl":""},"authentication":{"issuer":"","clientId":"","clientSecretEnv":"REQUESTARR_OIDC_CLIENT_SECRET"},"providers":{"tmdbEnabled":false,"tmdbTokenEnv":"REQUESTARR_TMDB_READ_TOKEN","musicBrainzEnabled":false,"musicBrainzContact":"","igdbEnabled":false,"igdbClientId":"","igdbClientSecretEnv":"REQUESTARR_IGDB_CLIENT_SECRET"}}}`},
		{"POST", "validate", `{"expectedRevision":1,"target":"general"}`},
	} {
		for _, role := range []string{"", "viewer", "requester", "admin"} {
			t.Run(endpoint.method+endpoint.path+role, func(t *testing.T) {
				service := &fakeSetup{}
				w := setupHTTP(service, role, endpoint.method, endpoint.path, endpoint.body, true)
				if role == "" {
					assertErrorEnvelope(t, w, 401, "authentication_required")
				} else if role != "admin" {
					assertErrorEnvelope(t, w, 403, "forbidden")
				} else if w.Code != 200 || service.actor.ID != matrixUserID {
					t.Fatalf("admin failed: %d %s", w.Code, w.Body.String())
				}
				if role != "admin" && service.calls != 0 {
					t.Fatal("non-admin reached setup service")
				}
			})
		}
	}
	service := &fakeSetup{}
	body := `{"expectedRevision":1,"currentStep":"general","settings":{"authentication":{"clientSecret":"never-store-this"}}}`
	w := setupHTTP(service, "admin", "PUT", "draft", body, true)
	assertErrorEnvelope(t, w, 400, "invalid_setup_input")
	if service.calls != 0 || strings.Contains(w.Body.String(), "never-store-this") {
		t.Fatal("secret payload accepted or reflected")
	}
	for _, tc := range []struct{ method, path, body string }{{"PUT", "draft", `{"expectedRevision":1}`}, {"POST", "validate", `{"expectedRevision":1,"target":"general"}`}} {
		assertErrorEnvelope(t, setupHTTP(&fakeSetup{}, "admin", tc.method, tc.path, tc.body, false), 403, "csrf_invalid")
	}
	w = setupHTTP(&fakeSetup{}, "admin", "GET", "draft", "", true)
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("draft response is cacheable")
	}
	var result map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || result["inactive"] != true {
		t.Fatalf("draft contract: %s err=%v", w.Body.String(), err)
	}
}

func TestSetupDraftRejectsMissingOrNullRequiredFields(t *testing.T) {
	valid := `{"expectedRevision":1,"currentStep":"general","settings":{"general":{"instanceName":"","timezone":"","publicUrl":""},"authentication":{"issuer":"","clientId":"","clientSecretEnv":"REQUESTARR_OIDC_CLIENT_SECRET"},"providers":{"tmdbEnabled":false,"tmdbTokenEnv":"REQUESTARR_TMDB_READ_TOKEN","musicBrainzEnabled":false,"musicBrainzContact":"","igdbEnabled":false,"igdbClientId":"","igdbClientSecretEnv":"REQUESTARR_IGDB_CLIENT_SECRET"}}}`
	cases := map[string]string{
		"settings omitted":       strings.Replace(valid, `,"settings":{"general":{"instanceName":"","timezone":"","publicUrl":""},"authentication":{"issuer":"","clientId":"","clientSecretEnv":"REQUESTARR_OIDC_CLIENT_SECRET"},"providers":{"tmdbEnabled":false,"tmdbTokenEnv":"REQUESTARR_TMDB_READ_TOKEN","musicBrainzEnabled":false,"musicBrainzContact":"","igdbEnabled":false,"igdbClientId":"","igdbClientSecretEnv":"REQUESTARR_IGDB_CLIENT_SECRET"}}`, "", 1),
		"settings null":          strings.Replace(valid, `"settings":{`, `"settings":null`, 1),
		"general omitted":        strings.Replace(valid, `"general":{"instanceName":"","timezone":"","publicUrl":""},`, "", 1),
		"general null":           strings.Replace(valid, `"general":{"instanceName":"","timezone":"","publicUrl":""}`, `"general":null`, 1),
		"authentication omitted": strings.Replace(valid, `"authentication":{"issuer":"","clientId":"","clientSecretEnv":"REQUESTARR_OIDC_CLIENT_SECRET"},`, "", 1),
		"authentication null":    strings.Replace(valid, `"authentication":{"issuer":"","clientId":"","clientSecretEnv":"REQUESTARR_OIDC_CLIENT_SECRET"}`, `"authentication":null`, 1),
		"providers omitted":      strings.Replace(valid, `,"providers":{"tmdbEnabled":false,"tmdbTokenEnv":"REQUESTARR_TMDB_READ_TOKEN","musicBrainzEnabled":false,"musicBrainzContact":"","igdbEnabled":false,"igdbClientId":"","igdbClientSecretEnv":"REQUESTARR_IGDB_CLIENT_SECRET"}`, "", 1),
		"providers null":         strings.Replace(valid, `"providers":{"tmdbEnabled":false,"tmdbTokenEnv":"REQUESTARR_TMDB_READ_TOKEN","musicBrainzEnabled":false,"musicBrainzContact":"","igdbEnabled":false,"igdbClientId":"","igdbClientSecretEnv":"REQUESTARR_IGDB_CLIENT_SECRET"}`, `"providers":null`, 1),
	}
	for _, field := range []string{`"instanceName":""`, `"timezone":""`, `"publicUrl":""`, `"issuer":""`, `"clientId":""`, `"clientSecretEnv":"REQUESTARR_OIDC_CLIENT_SECRET"`, `"tmdbEnabled":false`, `"tmdbTokenEnv":"REQUESTARR_TMDB_READ_TOKEN"`, `"musicBrainzEnabled":false`, `"musicBrainzContact":""`, `"igdbEnabled":false`, `"igdbClientId":""`, `"igdbClientSecretEnv":"REQUESTARR_IGDB_CLIENT_SECRET"`} {
		key := strings.Split(strings.Trim(field, `"`), `":`)[0]
		cases["field omitted "+key] = editSetupJSON(valid, key, false)
		cases["field null "+key] = editSetupJSON(valid, key, true)
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			service := &fakeSetup{}
			w := setupHTTP(service, "admin", "PUT", "draft", body, true)
			assertErrorEnvelope(t, w, 400, "invalid_setup_input")
			if service.calls != 0 {
				t.Fatal("invalid draft reached setup service")
			}
		})
	}

	service := &fakeSetup{}
	w := setupHTTP(service, "admin", "PUT", "draft", valid, true)
	if w.Code != 200 || service.calls != 1 {
		t.Fatalf("explicit empty strings/false toggles rejected: %d %s", w.Code, w.Body.String())
	}
}

func editSetupJSON(body, key string, setNull bool) string {
	var root map[string]any
	if err := json.Unmarshal([]byte(body), &root); err != nil {
		panic(err)
	}
	settings := root["settings"].(map[string]any)
	for _, section := range []string{"general", "authentication", "providers"} {
		fields := settings[section].(map[string]any)
		if _, exists := fields[key]; exists {
			if setNull {
				fields[key] = nil
			} else {
				delete(fields, key)
			}
		}
	}
	encoded, err := json.Marshal(root)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}
