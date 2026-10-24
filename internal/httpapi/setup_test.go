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

func (f *fakeSetup) Finish(ctx context.Context, actor setup.Actor, _ int64) (setup.Draft, error) {
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

func validSetupDraftBody() string {
	return `{"expectedRevision":1,"currentStep":"general","settings":{"general":{"instanceName":"","timezone":"","publicUrl":""},"authentication":{"issuer":"","clientId":"","clientSecretEnv":"REQUESTARR_OIDC_CLIENT_SECRET"},"providers":{"tmdbEnabled":false,"tmdbTokenEnv":"REQUESTARR_TMDB_READ_TOKEN","musicBrainzEnabled":false,"musicBrainzContact":"","igdbEnabled":false,"igdbClientId":"","igdbClientSecretEnv":"REQUESTARR_IGDB_CLIENT_SECRET"},"notifications":{"appriseEnabled":false,"appriseUrl":"","appriseConfigKeyEnv":"REQUESTARR_APPRISE_CONFIG_KEY"},"policy":{"autoApproveMediaTypes":[]},"libraries":{"jellyfin":{"enabled":false,"url":"","mediaTypes":[],"tokenEnv":"REQUESTARR_JELLYFIN_API_KEY","usernameEnv":"","passwordEnv":""},"plex":{"enabled":false,"url":"","mediaTypes":[],"tokenEnv":"REQUESTARR_PLEX_TOKEN","usernameEnv":"","passwordEnv":""},"romm":{"enabled":false,"url":"","mediaTypes":[],"tokenEnv":"REQUESTARR_ROMM_TOKEN","usernameEnv":"","passwordEnv":""},"navidrome":{"enabled":false,"url":"","mediaTypes":[],"tokenEnv":"","usernameEnv":"REQUESTARR_NAVIDROME_USERNAME","passwordEnv":"REQUESTARR_NAVIDROME_PASSWORD"}}}}`
}

func TestSetupDraftEndpointsRequireAdminAndProtectSecrets(t *testing.T) {
	for _, endpoint := range []struct{ method, path, body string }{
		{"GET", "draft", ""},
		{"PUT", "draft", validSetupDraftBody()},
		{"POST", "validate", `{"expectedRevision":1,"target":"general"}`},
		{"POST", "finish", `{"expectedRevision":1}`},
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
	for _, tc := range []struct{ method, path, body string }{{"PUT", "draft", `{"expectedRevision":1}`}, {"POST", "validate", `{"expectedRevision":1,"target":"general"}`}, {"POST", "finish", `{"expectedRevision":1}`}} {
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
	valid := validSetupDraftBody()
	cases := map[string]string{
		"settings omitted":       editSetupSection(valid, "settings", false),
		"settings null":          editSetupSection(valid, "settings", true),
		"general omitted":        strings.Replace(valid, `"general":{"instanceName":"","timezone":"","publicUrl":""},`, "", 1),
		"general null":           strings.Replace(valid, `"general":{"instanceName":"","timezone":"","publicUrl":""}`, `"general":null`, 1),
		"authentication omitted": strings.Replace(valid, `"authentication":{"issuer":"","clientId":"","clientSecretEnv":"REQUESTARR_OIDC_CLIENT_SECRET"},`, "", 1),
		"authentication null":    strings.Replace(valid, `"authentication":{"issuer":"","clientId":"","clientSecretEnv":"REQUESTARR_OIDC_CLIENT_SECRET"}`, `"authentication":null`, 1),
		"providers omitted":      strings.Replace(valid, `,"providers":{"tmdbEnabled":false,"tmdbTokenEnv":"REQUESTARR_TMDB_READ_TOKEN","musicBrainzEnabled":false,"musicBrainzContact":"","igdbEnabled":false,"igdbClientId":"","igdbClientSecretEnv":"REQUESTARR_IGDB_CLIENT_SECRET"}`, "", 1),
		"providers null":         strings.Replace(valid, `"providers":{"tmdbEnabled":false,"tmdbTokenEnv":"REQUESTARR_TMDB_READ_TOKEN","musicBrainzEnabled":false,"musicBrainzContact":"","igdbEnabled":false,"igdbClientId":"","igdbClientSecretEnv":"REQUESTARR_IGDB_CLIENT_SECRET"}`, `"providers":null`, 1),
	}
	cases["notifications omitted"] = editSetupSection(valid, "notifications", false)
	cases["policy null"] = editSetupSection(valid, "policy", true)
	cases["libraries omitted"] = editSetupSection(valid, "libraries", false)
	cases["libraries null"] = editSetupSection(valid, "libraries", true)
	for _, source := range []string{"jellyfin", "plex", "romm", "navidrome"} {
		cases[source+" source omitted"] = editLibrarySection(valid, source, false)
		cases[source+" source null"] = editLibrarySection(valid, source, true)
	}
	for _, field := range []string{`"instanceName":""`, `"timezone":""`, `"publicUrl":""`, `"issuer":""`, `"clientId":""`, `"clientSecretEnv":"REQUESTARR_OIDC_CLIENT_SECRET"`, `"tmdbEnabled":false`, `"tmdbTokenEnv":"REQUESTARR_TMDB_READ_TOKEN"`, `"musicBrainzEnabled":false`, `"musicBrainzContact":""`, `"igdbEnabled":false`, `"igdbClientId":""`, `"igdbClientSecretEnv":"REQUESTARR_IGDB_CLIENT_SECRET"`, `"enabled":false`, `"url":""`, `"mediaTypes":[]`, `"tokenEnv":"REQUESTARR_JELLYFIN_API_KEY"`, `"usernameEnv":"REQUESTARR_NAVIDROME_USERNAME"`, `"passwordEnv":"REQUESTARR_NAVIDROME_PASSWORD"`} {
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
	libraries := settings["libraries"].(map[string]any)
	for _, kind := range []string{"jellyfin", "plex", "romm", "navidrome"} {
		fields := libraries[kind].(map[string]any)
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

func editSetupSection(body, key string, setNull bool) string {
	var root map[string]any
	if err := json.Unmarshal([]byte(body), &root); err != nil {
		panic(err)
	}
	if key == "settings" {
		if setNull {
			root[key] = nil
		} else {
			delete(root, key)
		}
	} else {
		settings := root["settings"].(map[string]any)
		if setNull {
			settings[key] = nil
		} else {
			delete(settings, key)
		}
	}
	encoded, err := json.Marshal(root)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

func editLibrarySection(body, key string, setNull bool) string {
	var root map[string]any
	if err := json.Unmarshal([]byte(body), &root); err != nil {
		panic(err)
	}
	settings := root["settings"].(map[string]any)
	libraries := settings["libraries"].(map[string]any)
	if setNull {
		libraries[key] = nil
	} else {
		delete(libraries, key)
	}
	encoded, err := json.Marshal(root)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

func TestSetupFinishMapsServiceOutcomes(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{
		{setup.ErrNotReady, 422, "setup_not_ready"},
		{setup.ErrCompleted, 409, "setup_already_complete"},
		{setup.ErrConflict, 409, "setup_revision_conflict"},
	} {
		w := setupHTTP(&fakeSetup{err: tc.err}, "admin", "POST", "finish", `{"expectedRevision":1}`, true)
		assertErrorEnvelope(t, w, tc.status, tc.code)
	}
	assertErrorEnvelope(t, setupHTTP(&fakeSetup{}, "admin", "POST", "finish", `{"expectedRevision":1,"complete":true}`, true), 400, "invalid_setup_input")
}
