package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/evilperson1337/requestarr/internal/auth"
	"github.com/evilperson1337/requestarr/internal/auth/oidc"
	"github.com/evilperson1337/requestarr/internal/authsettings"
	"github.com/evilperson1337/requestarr/internal/buildinfo"
	"github.com/evilperson1337/requestarr/internal/secrets"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"
)

type authenticationFake struct{}

func (authenticationFake) Status(context.Context) (authsettings.Status, error) {
	return authsettings.Status{RedirectURI: "https://app.test/api/v1/auth/callback"}, nil
}
func (authenticationFake) Options() authsettings.Options {
	return authsettings.Options{OIDCEnabled: true, ProviderName: "Keycloak"}
}
func (authenticationFake) Save(context.Context, string, authsettings.Update) error { return nil }
func (authenticationFake) Test(context.Context, authsettings.Update) error         { return nil }
func (authenticationFake) SetEnabled(context.Context, string, bool) error          { return nil }
func (authenticationFake) Discard(context.Context, string) error                   { return nil }
func TestPublicSignInOptionsAreMinimal(t *testing.T) {
	d := testDeps(testLogger(), buildinfo.Info{}, fakeReadiness{}, nil, nil, nil)
	d.Authentication = authenticationFake{}
	h := mustRouter(d)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/auth/options", nil))
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("public options unavailable or cacheable")
	}
	var got map[string]any
	if json.Unmarshal(w.Body.Bytes(), &got) != nil || len(got) != 2 || got["oidcEnabled"] != true || got["providerName"] != "Keycloak" {
		t.Fatal("public options must reveal only enabled and name")
	}
}

type matrixAuthentication struct{ b *matrixBackend }

func (m matrixAuthentication) Options() authsettings.Options { return authsettings.Options{} }
func (m matrixAuthentication) Status(context.Context) (authsettings.Status, error) {
	return authsettings.Status{}, nil
}
func (m matrixAuthentication) Save(_ context.Context, actor string, _ authsettings.Update) error {
	m.b.record("authentication.save", actor)
	return nil
}
func (m matrixAuthentication) Test(context.Context, authsettings.Update) error {
	m.b.record("authentication.test", matrixUserID)
	return nil
}
func (m matrixAuthentication) SetEnabled(_ context.Context, actor string, _ bool) error {
	if m.b.authRefuses != nil {
		return m.b.authRefuses
	}
	m.b.record("authentication.enable", actor)
	return nil
}
func (m matrixAuthentication) Discard(_ context.Context, actor string) error {
	if m.b.authRefuses != nil {
		return m.b.authRefuses
	}
	m.b.record("authentication.discard", actor)
	return nil
}

func TestOIDCSecretCannotBypassAuthenticationSettings(t *testing.T) {
	for _, method := range []string{"PUT", "DELETE"} {
		endpoint := mutationCase{pattern: method + " /admin/secrets/{name}", path: "/admin/secrets/oidc_client_secret", body: `{"value":"canary-secret"}`}
		backend := &matrixBackend{t: t}
		w := matrixRequest(backend, endpoint, "admin", "csrf-token", "http://example.test")
		if w.Code != 409 || len(backend.effects) != 0 {
			t.Fatal("OIDC secret must be managed atomically through Authentication settings")
		}
	}
}

func TestRecoveryManagementRoutesRequireAdministrator(t *testing.T) {
	for _, role := range []string{"unauthenticated", "viewer", "requester", "admin"} {
		endpoint := mutationCase{pattern: "GET /admin/settings/authentication/local-administrator", path: "/admin/settings/authentication/local-administrator"}
		b := &matrixBackend{t: t}
		w := matrixRequest(b, endpoint, role, "csrf-token", "http://example.test")
		want := 200
		if role == "unauthenticated" {
			want = 401
		} else if role != "admin" {
			want = 403
		}
		if w.Code != want {
			t.Fatal("recovery management read role policy is incorrect")
		}
	}
}

type matrixLocalManagement struct{ b *matrixBackend }

func (m matrixLocalManagement) LocalAdminStatus(context.Context) (auth.LocalAdminStatus, error) {
	return auth.LocalAdminStatus{Exists: true}, nil
}
func (m matrixLocalManagement) Create(_ context.Context, actor string) (auth.CreatedLocalAdmin, error) {
	if m.b != nil {
		m.b.record("localadmin.create", actor)
	}
	return auth.CreatedLocalAdmin{Username: "admin", Password: "test-once-password", MustChangePassword: true}, nil
}
func (m matrixLocalManagement) SetDisabled(_ context.Context, actor string, _ bool) error {
	if m.b != nil {
		m.b.record("localadmin.disable", actor)
	}
	return nil
}

func TestAuthenticationSettingsReadRoleMatrix(t *testing.T) {
	for _, role := range []string{"unauthenticated", "viewer", "requester", "admin"} {
		endpoint := mutationCase{pattern: "GET /admin/settings/authentication", path: "/admin/settings/authentication"}
		b := &matrixBackend{t: t}
		w := matrixRequest(b, endpoint, role, "csrf-token", "http://example.test")
		want := 200
		if role == "unauthenticated" {
			want = 401
		} else if role != "admin" {
			want = 403
		}
		if w.Code != want {
			t.Fatal("authentication settings read role policy is incorrect")
		}
	}
}
func TestAuthenticationResponseSchemasMatchContract(t *testing.T) {
	for schema, want := range map[string][]string{
		"SignInOptions":                {"oidcEnabled", "providerName"},
		"OIDCSettings":                 {"enabled", "providerName", "issuer", "clientId"},
		"AuthenticationSettingsUpdate": {"enabled", "providerName", "issuer", "clientId", "clientSecret"},
		"AuthenticationSettings":       {"state", "missing", "ignored", "differing", "saved", "effective", "secretConfigured", "secretUnreadable", "canSaveSecret", "readOnly", "redirectUri", "working", "problem"},
		"LocalAdministratorStatus":     {"exists", "disabled", "canDisable", "otherAdminExists", "oidcWorking"},
	} {
		got := specProperties(t, schema)
		sort.Strings(got)
		sort.Strings(want)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("schema %s property mismatch", schema)
		}
	}
}

type oidcSettingsMemory struct{ record *authsettings.Record }

func (m *oidcSettingsMemory) LoadOIDC(context.Context) (*authsettings.Record, error) {
	return m.record, nil
}
func (m *oidcSettingsMemory) SaveOIDC(_ context.Context, _ string, r *authsettings.Record) error {
	m.record = r
	return nil
}
func TestAuthenticationAPIReportsFailureWithoutReplacingWorkingProvider(t *testing.T) {
	ctx := context.Background()
	store := &oidcSettingsMemory{}
	sealer, _ := secrets.NewAESGCM(bytes.Repeat([]byte{2}, 32))
	svc, _ := authsettings.New(ctx, store, sealer, authsettings.Values{}, "http://example.test", func(_ context.Context, v authsettings.Values, _ string) (oidc.Provider, error) {
		if v.ClientID == "broken" {
			return nil, errors.New("upstream canary-secret")
		}
		return authProviderForSettings{}, nil
	})
	repo := csrfAuthRepo{statusAuthRepo{user: auth.User{ID: matrixUserID, Role: "admin"}}}
	d := testDeps(testLogger(), buildinfo.Info{}, fakeReadiness{}, nil, nil, nil, &auth.Handler{Repo: repo, Secret: "test-secret", PublicURL: "http://example.test"})
	d.Authentication = svc
	h := mustRouter(d)
	good := `{"enabled":true,"providerName":"Keycloak","issuer":"https://id.test","clientId":"working","clientSecret":"canary-secret"}`
	w := localCall(h, "PUT", "/admin/settings/authentication", good, true)
	if w.Code != 200 || strings.Contains(w.Body.String(), "canary-secret") {
		t.Fatal("successful save leaked secret or failed")
	}
	old := svc.Snapshot()
	w = localCall(h, "PUT", "/admin/settings/authentication", strings.Replace(good, "working", "broken", 1), true)
	if w.Code != 422 || strings.Contains(w.Body.String(), "canary-secret") || svc.Snapshot() != old || store.record.Settings.ClientID != "working" {
		t.Fatal("failed API save replaced working config or exposed secret")
	}
	w = localCall(h, "GET", "/admin/settings/authentication", "", true)
	if w.Code != 200 || strings.Contains(w.Body.String(), "canary-secret") || !strings.Contains(w.Body.String(), `"working":true`) {
		t.Fatal("status lost previous live provider")
	}
	// Strict bounded input, including null and trailing values, never mutates.
	for _, body := range []string{`null`, `{} {}`, `{"unknown":true}`, strings.Repeat("x", 8193)} {
		w = localCall(h, "PUT", "/admin/settings/authentication", body, true)
		if w.Code < 400 || svc.Snapshot() != old {
			t.Fatal("invalid body was accepted")
		}
	}
}

type authProviderForSettings struct{}

func (authProviderForSettings) AuthorizationURL(string, string, string) string {
	return "https://id.test"
}
func (authProviderForSettings) Exchange(context.Context, string, string, string) (*oidc.Identity, error) {
	return nil, nil
}

// Turning OIDC off while the local administrator is disabled would lock everyone
// out, so the service refuses and the API says why, with a conflict and no effect.
func TestTurningOIDCOffIsRefusedWhenItWouldLeaveNoSignIn(t *testing.T) {
	for _, endpoint := range []mutationCase{
		{pattern: "PUT /admin/settings/authentication/enabled", path: "/admin/settings/authentication/enabled", body: `{"enabled":false}`},
		{pattern: "DELETE /admin/settings/authentication", path: "/admin/settings/authentication"},
	} {
		backend := &matrixBackend{t: t, authRefuses: authsettings.ErrNoOtherSignIn}
		w := matrixRequest(backend, endpoint, "admin", "csrf-token", "http://example.test")
		assertErrorEnvelope(t, w, 409, "oidc_is_only_sign_in")
		if len(backend.effects) != 0 {
			t.Fatalf("%s must change nothing when it is refused", endpoint.pattern)
		}
	}
}
