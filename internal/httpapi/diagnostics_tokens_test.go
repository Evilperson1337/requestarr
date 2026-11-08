package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// session sends a request as the signed-in user, with the CSRF cookie and
// header and the same Origin unless the caller changes them.
func (f *bearerFixture) session(method, path, body string, mutate ...func(*http.Request)) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/api/v1"+path, strings.NewReader(body))
	r.RemoteAddr = "10.0.0.5:4000"
	r.AddCookie(&http.Cookie{Name: "requestarr_session", Value: "valid"})
	r.AddCookie(&http.Cookie{Name: "requestarr_csrf", Value: "csrf-token"})
	r.Header.Set("X-CSRF-Token", "csrf-token")
	r.Header.Set("Origin", "http://example.test")
	r.Header.Set("Content-Type", "application/json")
	for _, m := range mutate {
		m(r)
	}
	w := httptest.NewRecorder()
	f.h.ServeHTTP(w, r)
	return w
}

type tokenRow struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	ExpiresAt string `json:"expiresAt"`
}

func TestASuperuserCreatesListsAndRevokesATokenAndTheValueAppearsOnlyOnCreation(t *testing.T) {
	f := newBearerFixture(t)
	w := f.session("POST", "/admin/diagnostics-tokens", `{"name":"laptop","lifetimeDays":7}`)
	if w.Code != 201 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("create: %d %v %s", w.Code, w.Header(), w.Body)
	}
	var created struct {
		Value string   `json:"value"`
		Token tokenRow `json:"token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil || !strings.HasPrefix(created.Value, "rqd_") || created.Token.Name != "laptop" {
		t.Fatalf("create body: %v %s", err, w.Body)
	}
	if want := "2026-11-15T12:00:00Z"; created.Token.ExpiresAt != want {
		t.Errorf("expiresAt = %s, want %s (seven days)", created.Token.ExpiresAt, want)
	}
	if got := f.get("/admin/logs", "Bearer "+created.Value); got.Code != 200 {
		t.Errorf("new token on /admin/logs: %d", got.Code)
	}
	list := f.session("GET", "/admin/diagnostics-tokens", "")
	if list.Code != 200 || list.Header().Get("Cache-Control") != "no-store" || strings.Contains(list.Body.String(), created.Value) || strings.Contains(list.Body.String(), f.token) {
		t.Fatalf("list: %d %v %s", list.Code, list.Header(), list.Body)
	}
	var listed struct {
		Tokens []tokenRow `json:"tokens"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &listed); err != nil || len(listed.Tokens) != 2 {
		t.Fatalf("list body: %v %s", err, list.Body)
	}
	del := f.session("DELETE", "/admin/diagnostics-tokens/"+created.Token.ID, "")
	if del.Code != 204 || del.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("revoke: %d %v", del.Code, del.Header())
	}
	if got := f.get("/admin/logs", "Bearer "+created.Value); got.Code != 401 {
		t.Errorf("revoked token on /admin/logs: %d, want 401", got.Code)
	}
	assertErrorEnvelope(t, f.session("DELETE", "/admin/diagnostics-tokens/"+created.Token.ID, ""), 404, "diagnostics_token_not_found")
}

func TestCreatingATokenChecksTheInput(t *testing.T) {
	f := newBearerFixture(t)
	for _, c := range []struct {
		name, body, code string
		status           int
	}{
		{"no name", `{"lifetimeDays":7}`, "invalid_diagnostics_token", 422},
		{"blank name", `{"name":"   "}`, "invalid_diagnostics_token", 422},
		{"too long", `{"name":"` + strings.Repeat("a", 81) + `"}`, "invalid_diagnostics_token", 422},
		{"too many days", `{"name":"x","lifetimeDays":91}`, "invalid_diagnostics_token", 422},
		{"negative days", `{"name":"x","lifetimeDays":-1}`, "invalid_diagnostics_token", 422},
		{"unknown field", `{"name":"x","admin":true}`, "invalid_diagnostics_token_input", 400},
		{"not json", `nope`, "invalid_diagnostics_token_input", 400},
	} {
		t.Run(c.name, func(t *testing.T) {
			assertErrorEnvelope(t, f.session("POST", "/admin/diagnostics-tokens", c.body), c.status, c.code)
		})
	}
	var created struct {
		Token tokenRow `json:"token"`
	}
	w := f.session("POST", "/admin/diagnostics-tokens", `{"name":"default"}`)
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil || created.Token.ExpiresAt != "2026-12-08T12:00:00Z" {
		t.Errorf("default lifetime (30 days): %v %s", err, w.Body)
	}
}

func TestRevokingAMalformedIdIsNotFound(t *testing.T) {
	f := newBearerFixture(t)
	assertErrorEnvelope(t, f.session("DELETE", "/admin/diagnostics-tokens/not-a-uuid", ""), 404, "diagnostics_token_not_found")
}

func TestTheLimitOnActiveTokensIsAConflict(t *testing.T) {
	f := newBearerFixture(t)
	f.store.limit = true
	assertErrorEnvelope(t, f.session("POST", "/admin/diagnostics-tokens", `{"name":"six"}`), 409, "diagnostics_token_limit")
}

func TestTokenManagementChangesNeedCSRFAndSameOrigin(t *testing.T) {
	f := newBearerFixture(t)
	for _, c := range []struct {
		name, code string
		mutate     func(*http.Request)
	}{
		{"missing csrf", "csrf_invalid", func(r *http.Request) { r.Header.Del("X-CSRF-Token") }},
		{"wrong csrf", "csrf_invalid", func(r *http.Request) { r.Header.Set("X-CSRF-Token", "other") }},
		{"foreign origin", "origin_rejected", func(r *http.Request) { r.Header.Set("Origin", "http://evil.test") }},
		{"missing origin", "origin_rejected", func(r *http.Request) { r.Header.Del("Origin") }},
	} {
		for _, call := range []struct{ method, path, body string }{
			{"POST", "/admin/diagnostics-tokens", `{"name":"x"}`},
			{"DELETE", "/admin/diagnostics-tokens/" + f.id, ""},
		} {
			assertErrorEnvelope(t, f.session(call.method, call.path, call.body, c.mutate), 403, c.code)
		}
	}
	if got := f.get("/admin/logs", "Bearer "+f.token); got.Code != 200 || len(f.store.rows) != 1 {
		t.Errorf("a refused request changed tokens: %d, %d rows", got.Code, len(f.store.rows))
	}
}

func TestOnlyASuperuserManagesTokens(t *testing.T) {
	for _, role := range []string{"admin", "requester", "viewer"} {
		f := newBearerFixtureAs(t, role)
		for _, call := range []struct{ method, path, body string }{
			{"GET", "/admin/diagnostics-tokens", ""},
			{"POST", "/admin/diagnostics-tokens", `{"name":"x"}`},
			{"DELETE", "/admin/diagnostics-tokens/" + f.id, ""},
		} {
			if w := f.session(call.method, call.path, call.body); w.Code != 403 {
				t.Errorf("%s %s %s: %d, want 403", role, call.method, call.path, w.Code)
			}
		}
	}
	f := newBearerFixture(t)
	for _, call := range []struct{ method, path string }{{"GET", "/admin/diagnostics-tokens"}, {"POST", "/admin/diagnostics-tokens"}, {"DELETE", "/admin/diagnostics-tokens/" + f.id}} {
		if w := f.call(call.method, call.path, "", "10.0.0.8"); w.Code != 401 {
			t.Errorf("anonymous %s %s: %d, want 401", call.method, call.path, w.Code)
		}
	}
}

func TestATokenCannotManageTokens(t *testing.T) {
	f := newBearerFixture(t)
	for _, call := range []struct{ method, path string }{{"GET", "/admin/diagnostics-tokens"}, {"POST", "/admin/diagnostics-tokens"}, {"DELETE", "/admin/diagnostics-tokens/" + f.id}} {
		assertErrorEnvelope(t, f.call(call.method, call.path, "Bearer "+f.token, "10.0.0.9"), 401, "authentication_required")
	}
	if len(f.store.rows) != 1 {
		t.Errorf("a token changed the token table: %d rows", len(f.store.rows))
	}
}

func TestTokenManagementRoutesArePolicedSuperuserOnlyWithCSRF(t *testing.T) {
	f := newBearerFixture(t)
	want := map[string]bool{"GET /admin/diagnostics-tokens": false, "POST /admin/diagnostics-tokens": true, "DELETE /admin/diagnostics-tokens/{id}": true}
	for _, p := range f.rt.Policies() {
		csrf, ok := want[p.Pattern]
		if !ok {
			continue
		}
		delete(want, p.Pattern)
		if p.Access != accessSuperuser || p.CSRF != csrf || p.TokenReadable {
			t.Errorf("%s = %+v", p.Pattern, p)
		}
	}
	if len(want) != 0 {
		t.Errorf("not registered: %v", want)
	}
}

// The service is optional in the router (tests build it without one); the
// routes then answer that tokens are unavailable rather than panic.
func TestTokenRoutesWithoutAServiceAreUnavailable(t *testing.T) {
	f := newBearerFixture(t)
	f.rt.auth.Diagnostics = nil
	assertErrorEnvelope(t, f.session("GET", "/admin/diagnostics-tokens", ""), 503, "diagnostics_tokens_unavailable")
}
