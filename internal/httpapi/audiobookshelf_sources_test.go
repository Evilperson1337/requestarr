package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"git.example.com/evilperson1337/Requestarr/internal/auth"
	"git.example.com/evilperson1337/Requestarr/internal/inventory"
	"git.example.com/evilperson1337/Requestarr/internal/secrets"
)

func TestAudiobookshelfSourcesListIsLocalAndSecretFree(t *testing.T) {
	query := &sourceQueryFake{views: []inventory.SourceConfigurationView{{Configuration: inventory.SourceConfiguration{ID: "11111111-1111-4111-8111-111111111111", Name: "Books", Kind: inventory.SourceAudiobookshelf, MediaTypes: nil}, EndpointURL: "https://abs.example", Revision: 1, CredentialConfigured: true, Status: inventory.SourceStatus{State: inventory.SourceSyncIdle}}}}
	handler := sourceHandler(t, AudiobookshelfSourceOptions{Query: query})
	response := serveAdminRequest(t, handler, http.MethodGet, "/api/v1/admin/inventory/audiobookshelf/sources", "")
	if response.Code != http.StatusOK || query.listCalls != 1 || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status=%d calls=%d cache=%q", response.Code, query.listCalls, response.Header().Get("Cache-Control"))
	}
	if strings.Contains(response.Body.String(), "credential") && strings.Contains(response.Body.String(), "secret") {
		t.Fatalf("response exposed credential: %s", response.Body.String())
	}
}

func TestAudiobookshelfConnectionTestUsesCandidateOnly(t *testing.T) {
	probe := &sourceProbeFake{result: inventory.ConnectionProbeResult{Libraries: []inventory.RemoteLibrary{{ID: "library", Name: "Books"}}}}
	handler := sourceHandler(t, AudiobookshelfSourceOptions{Probe: probe})
	body := `{"endpointURL":"https://abs.example","credential":"candidate-secret"}`
	response := serveAdminRequest(t, handler, http.MethodPost, "/api/v1/admin/inventory/audiobookshelf/sources/connection-test", body)
	if response.Code != http.StatusOK || probe.calls != 1 || probe.request.Credential != "candidate-secret" {
		t.Fatalf("status=%d calls=%d request=%+v", response.Code, probe.calls, probe.request)
	}
	if strings.Contains(response.Body.String(), "candidate-secret") {
		t.Fatal("probe credential was returned")
	}
}

func TestAudiobookshelfSyncRecordsIntentOnly(t *testing.T) {
	sync := &manualSyncFake{result: inventory.ManualSyncRequestResult{Disposition: inventory.ManualSyncRecorded, PendingIntentID: "22222222-2222-4222-8222-222222222222"}}
	handler := sourceHandler(t, AudiobookshelfSourceOptions{ManualSync: sync})
	response := serveAdminRequest(t, handler, http.MethodPost, "/api/v1/admin/inventory/audiobookshelf/sources/11111111-1111-4111-8111-111111111111/sync", `{}`)
	if response.Code != http.StatusAccepted || sync.calls != 1 || sync.request.SourceID != "11111111-1111-4111-8111-111111111111" || sync.request.RequestedBy != "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa" || sync.request.ID == "" || sync.request.RequestedAt.IsZero() {
		t.Fatalf("status=%d request=%+v", response.Code, sync.request)
	}
}

func TestAudiobookshelfSourceMutationRejectsInvalidCSRFAndBody(t *testing.T) {
	handler := sourceHandler(t, AudiobookshelfSourceOptions{Probe: &sourceProbeFake{}})
	valid := `{"endpointURL":"https://abs.example","credential":"candidate"}`
	for _, testCase := range []struct {
		name   string
		mutate func(*http.Request)
		want   int
	}{
		{name: "method", mutate: func(r *http.Request) { r.Method = http.MethodGet }, want: http.StatusMethodNotAllowed},
		{name: "origin", mutate: func(r *http.Request) { r.Header.Del("Origin") }, want: http.StatusForbidden},
		{name: "csrf", mutate: func(r *http.Request) { r.Header.Del("X-Requestarr-CSRF") }, want: http.StatusForbidden},
		{name: "content type", mutate: func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }, want: http.StatusBadRequest},
		{name: "unknown field", mutate: func(r *http.Request) {
			r.Body = io.NopCloser(strings.NewReader(valid[:len(valid)-1] + `,"unknown":true}`))
		}, want: http.StatusBadRequest},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/inventory/audiobookshelf/sources/connection-test", strings.NewReader(valid))
			request.Header.Set("Origin", "https://requestarr.example")
			request.Header.Set("X-Requestarr-CSRF", "1")
			request.Header.Set("Content-Type", "application/json")
			request.AddCookie(&http.Cookie{Name: authSessionCookieName(false), Value: authTestSessionToken})
			testCase.mutate(request)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != testCase.want || response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("status=%d cache=%q", response.Code, response.Header().Get("Cache-Control"))
			}
		})
	}
}

func TestAudiobookshelfSourceMutationsRequireExpectedRevision(t *testing.T) {
	save := &sourceSaveFake{result: inventory.SourceSaveResult{Revision: 1}}
	handler := sourceHandler(t, AudiobookshelfSourceOptions{Save: save})
	for _, testCase := range []struct {
		name, method, target, body string
	}{
		{"create omitted", http.MethodPost, "/api/v1/admin/inventory/audiobookshelf/sources", `{"name":"Books","endpointURL":"https://abs.example","enabled":true,"selectedLibraries":["books"],"credentialMode":"replace","credential":"secret"}`},
		{"update omitted", http.MethodPut, "/api/v1/admin/inventory/audiobookshelf/sources/11111111-1111-4111-8111-111111111111", `{"name":"Books","endpointURL":"https://abs.example","enabled":true,"selectedLibraries":["books"],"credentialMode":"preserve"}`},
		{"create nonzero", http.MethodPost, "/api/v1/admin/inventory/audiobookshelf/sources", `{"name":"Books","endpointURL":"https://abs.example","enabled":true,"selectedLibraries":["books"],"credentialMode":"replace","credential":"secret","expectedRevision":1}`},
		{"update zero", http.MethodPut, "/api/v1/admin/inventory/audiobookshelf/sources/11111111-1111-4111-8111-111111111111", `{"name":"Books","endpointURL":"https://abs.example","enabled":true,"selectedLibraries":["books"],"credentialMode":"preserve","expectedRevision":0}`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			response := serveAdminRequest(t, handler, testCase.method, testCase.target, testCase.body)
			if response.Code != http.StatusUnprocessableEntity || save.calls != 0 {
				t.Fatalf("status=%d save calls=%d", response.Code, save.calls)
			}
		})
	}
}

func TestAudiobookshelfSourceCredentialModesAndIDs(t *testing.T) {
	save := &sourceSaveFake{result: inventory.SourceSaveResult{Revision: 2}}
	handler := sourceHandler(t, AudiobookshelfSourceOptions{Save: save, NewID: fixedIDGenerator("22222222-2222-4222-8222-222222222222", "33333333-3333-4333-8333-333333333333", "44444444-4444-4444-8444-444444444444")})
	create := `{"name":"Books","endpointURL":"https://abs.example","enabled":true,"selectedLibraries":["books"],"credentialMode":"replace","credential":"secret","expectedRevision":0}`
	if response := serveAdminRequest(t, handler, http.MethodPost, "/api/v1/admin/inventory/audiobookshelf/sources", create); response.Code != http.StatusCreated || save.calls != 1 || save.request.Configuration.ID != "22222222-2222-4222-8222-222222222222" || save.request.AuditID != "33333333-3333-4333-8333-333333333333" {
		t.Fatalf("create status=%d save=%+v", response.Code, save.request)
	}
	preserve := `{"name":"Books","endpointURL":"https://abs.example","enabled":true,"selectedLibraries":["books"],"credentialMode":"preserve","expectedRevision":2}`
	if response := serveAdminRequest(t, handler, http.MethodPut, "/api/v1/admin/inventory/audiobookshelf/sources/11111111-1111-4111-8111-111111111111", preserve); response.Code != http.StatusOK || !save.request.Credential.Preserve {
		t.Fatalf("preserve status=%d credential=%+v", response.Code, save.request.Credential)
	}
	if response := serveAdminRequest(t, handler, http.MethodPut, "/api/v1/admin/inventory/audiobookshelf/sources/not-a-uuid", preserve); response.Code != http.StatusBadRequest || save.calls != 2 {
		t.Fatalf("bad id status=%d calls=%d", response.Code, save.calls)
	}
}

func TestAudiobookshelfSourceIDGenerationFailureDoesNotInvokeServices(t *testing.T) {
	boom := func() (string, error) { return "", errors.New("randomness unavailable") }
	save := &sourceSaveFake{}
	sync := &manualSyncFake{}
	handler := sourceHandler(t, AudiobookshelfSourceOptions{Save: save, ManualSync: sync, NewID: boom})
	create := `{"name":"Books","endpointURL":"https://abs.example","enabled":true,"selectedLibraries":["books"],"credentialMode":"replace","credential":"secret","expectedRevision":0}`
	if response := serveAdminRequest(t, handler, http.MethodPost, "/api/v1/admin/inventory/audiobookshelf/sources", create); response.Code != http.StatusServiceUnavailable || save.calls != 0 {
		t.Fatalf("create status=%d save calls=%d", response.Code, save.calls)
	}
	if response := serveAdminRequest(t, handler, http.MethodPost, "/api/v1/admin/inventory/audiobookshelf/sources/11111111-1111-4111-8111-111111111111/sync", `{}`); response.Code != http.StatusServiceUnavailable || sync.calls != 0 {
		t.Fatalf("sync status=%d sync calls=%d", response.Code, sync.calls)
	}
}

func TestAudiobookshelfSyncRequiresExactlyEmptyJSONObject(t *testing.T) {
	sync := &manualSyncFake{result: inventory.ManualSyncRequestResult{Disposition: inventory.ManualSyncRecorded, PendingIntentID: "22222222-2222-4222-8222-222222222222"}}
	handler := sourceHandler(t, AudiobookshelfSourceOptions{ManualSync: sync})
	for _, body := range []string{"null", "[]", `{"ignored":true}`, `{}`, `{} {}`} {
		response := serveAdminRequest(t, handler, http.MethodPost, "/api/v1/admin/inventory/audiobookshelf/sources/11111111-1111-4111-8111-111111111111/sync", body)
		want := http.StatusBadRequest
		if body == "{}" {
			want = http.StatusAccepted
		}
		if response.Code != want {
			t.Fatalf("body=%q status=%d want=%d", body, response.Code, want)
		}
	}
	if sync.calls != 1 {
		t.Fatalf("sync calls=%d", sync.calls)
	}
}

func TestAudiobookshelfSourceRouteMethodsAndPrecedence(t *testing.T) {
	const validID = "11111111-1111-4111-8111-111111111111"
	for _, testCase := range []struct {
		name, method, target string
		wantStatus           int
		wantAllow            []string
	}{
		{name: "collection", method: http.MethodDelete, target: "/api/v1/admin/inventory/audiobookshelf/sources", wantStatus: http.StatusMethodNotAllowed, wantAllow: []string{http.MethodGet, http.MethodPost}},
		{name: "collection options", method: http.MethodOptions, target: "/api/v1/admin/inventory/audiobookshelf/sources", wantStatus: http.StatusMethodNotAllowed, wantAllow: []string{http.MethodGet, http.MethodPost}},
		{name: "connection test", method: http.MethodGet, target: "/api/v1/admin/inventory/audiobookshelf/sources/connection-test", wantStatus: http.StatusMethodNotAllowed, wantAllow: []string{http.MethodPost}},
		{name: "connection test options", method: http.MethodOptions, target: "/api/v1/admin/inventory/audiobookshelf/sources/connection-test", wantStatus: http.StatusMethodNotAllowed, wantAllow: []string{http.MethodPost}},
		{name: "item", method: http.MethodPatch, target: "/api/v1/admin/inventory/audiobookshelf/sources/" + validID, wantStatus: http.StatusMethodNotAllowed, wantAllow: []string{http.MethodGet, http.MethodPut}},
		{name: "item options", method: http.MethodOptions, target: "/api/v1/admin/inventory/audiobookshelf/sources/" + validID, wantStatus: http.StatusMethodNotAllowed, wantAllow: []string{http.MethodGet, http.MethodPut}},
		{name: "sync", method: http.MethodGet, target: "/api/v1/admin/inventory/audiobookshelf/sources/" + validID + "/sync", wantStatus: http.StatusMethodNotAllowed, wantAllow: []string{http.MethodPost}},
		{name: "sync options", method: http.MethodOptions, target: "/api/v1/admin/inventory/audiobookshelf/sources/" + validID + "/sync", wantStatus: http.StatusMethodNotAllowed, wantAllow: []string{http.MethodPost}},
		{name: "item validates source ID before method", method: http.MethodDelete, target: "/api/v1/admin/inventory/audiobookshelf/sources/not-a-uuid", wantStatus: http.StatusBadRequest},
		{name: "sync validates method before source ID", method: http.MethodGet, target: "/api/v1/admin/inventory/audiobookshelf/sources/not-a-uuid/sync", wantStatus: http.StatusMethodNotAllowed, wantAllow: []string{http.MethodPost}},
		{name: "sync rejects malformed source ID after post selection", method: http.MethodPost, target: "/api/v1/admin/inventory/audiobookshelf/sources/not-a-uuid/sync", wantStatus: http.StatusBadRequest},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			probe := &sourceProbeFake{}
			query := &sourceQueryFake{}
			save := &sourceSaveFake{}
			sync := &manualSyncFake{}
			handler := sourceHandler(t, AudiobookshelfSourceOptions{Probe: probe, Query: query, Save: save, ManualSync: sync})
			response := serveAdminRequest(t, handler, testCase.method, testCase.target, `{"malformed":true}`)

			if response.Code != testCase.wantStatus || !sameStrings(response.Header().Values("Allow"), testCase.wantAllow) || sourceDownstreamCalls(probe, query, save, sync) != 0 || response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("X-Request-ID") == "" {
				t.Fatalf("status=%d allow=%q calls=%d cache=%q request-id=%q", response.Code, response.Header().Values("Allow"), sourceDownstreamCalls(probe, query, save, sync), response.Header().Get("Cache-Control"), response.Header().Get("X-Request-ID"))
			}
		})
	}
}

func TestAudiobookshelfSourceRouteFailurePrecedence(t *testing.T) {
	const validID = "11111111-1111-4111-8111-111111111111"
	for _, testCase := range []struct {
		name, method, target, body string
		cookie                     bool
		role                       auth.Role
		wantStatus                 int
		wantCode                   string
	}{
		{name: "anonymous authorization precedes method", method: http.MethodDelete, target: "/api/v1/admin/inventory/audiobookshelf/sources", wantStatus: http.StatusUnauthorized, wantCode: "unauthorized"},
		{name: "anonymous authorization precedes invalid item ID", method: http.MethodGet, target: "/api/v1/admin/inventory/audiobookshelf/sources/not-a-uuid", wantStatus: http.StatusUnauthorized, wantCode: "unauthorized"},
		{name: "requester authorization precedes invalid body", method: http.MethodPost, target: "/api/v1/admin/inventory/audiobookshelf/sources/connection-test", body: `{`, cookie: true, role: auth.RoleRequester, wantStatus: http.StatusForbidden, wantCode: "forbidden"},
		{name: "admin method precedes invalid sync ID and body", method: http.MethodGet, target: "/api/v1/admin/inventory/audiobookshelf/sources/not-a-uuid/sync", body: `{`, cookie: true, role: auth.RoleAdmin, wantStatus: http.StatusMethodNotAllowed, wantCode: "method_not_allowed"},
		{name: "admin item ID precedes method and body", method: http.MethodDelete, target: "/api/v1/admin/inventory/audiobookshelf/sources/not-a-uuid", body: `{`, cookie: true, role: auth.RoleAdmin, wantStatus: http.StatusBadRequest, wantCode: "invalid_request"},
		{name: "admin body follows selected connection test method", method: http.MethodPost, target: "/api/v1/admin/inventory/audiobookshelf/sources/connection-test", body: `{`, cookie: true, role: auth.RoleAdmin, wantStatus: http.StatusBadRequest, wantCode: "invalid_request"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			probe := &sourceProbeFake{}
			query := &sourceQueryFake{}
			save := &sourceSaveFake{}
			sync := &manualSyncFake{}
			handler := sourceHandlerWithAuth(t, AudiobookshelfSourceOptions{Probe: probe, Query: query, Save: save, ManualSync: sync}, nil, testCase.role)
			req := httptest.NewRequest(testCase.method, testCase.target, strings.NewReader(testCase.body))
			req.Header.Set("Origin", "https://requestarr.example")
			req.Header.Set("X-Requestarr-CSRF", "1")
			req.Header.Set("Content-Type", "application/json")
			if testCase.cookie {
				req.AddCookie(&http.Cookie{Name: authSessionCookieName(false), Value: authTestSessionToken})
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, req)

			if response.Code != testCase.wantStatus || responseErrorCode(t, response) != testCase.wantCode || sourceDownstreamCalls(probe, query, save, sync) != 0 || response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("X-Request-ID") == "" {
				t.Fatalf("status=%d code=%q calls=%d cache=%q request-id=%q", response.Code, responseErrorCode(t, response), sourceDownstreamCalls(probe, query, save, sync), response.Header().Get("Cache-Control"), response.Header().Get("X-Request-ID"))
			}
		})
	}
}

func TestAudiobookshelfConnectionTestAuthenticationFailures(t *testing.T) {
	const target = "/api/v1/admin/inventory/audiobookshelf/sources/connection-test"
	const body = `{"endpointURL":"https://abs.example","credential":"candidate-secret"}`

	for _, testCase := range []struct {
		name       string
		role       auth.Role
		cookie     bool
		probeErr   error
		wantStatus int
		wantCode   string
		wantCalls  int
	}{
		{name: "anonymous", wantStatus: http.StatusUnauthorized, wantCode: "unauthorized", wantCalls: 0},
		{name: "admin probe authentication failed", role: auth.RoleAdmin, cookie: true, probeErr: inventory.ErrProbeAuthenticationFailed, wantStatus: http.StatusUnauthorized, wantCode: "connection_test_authentication_failed", wantCalls: 1},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			probe := &sourceProbeFake{err: testCase.probeErr}
			handler := sourceHandlerWithAuth(t, AudiobookshelfSourceOptions{Probe: probe}, nil, testCase.role)
			req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
			req.Header.Set("Origin", "https://requestarr.example")
			req.Header.Set("X-Requestarr-CSRF", "1")
			req.Header.Set("Content-Type", "application/json")
			if testCase.cookie {
				req.AddCookie(&http.Cookie{Name: authSessionCookieName(false), Value: authTestSessionToken})
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, req)

			if response.Code != testCase.wantStatus || responseErrorCode(t, response) != testCase.wantCode || probe.calls != testCase.wantCalls {
				t.Fatalf("status=%d code=%q probe calls=%d", response.Code, responseErrorCode(t, response), probe.calls)
			}
		})
	}
}

func TestAudiobookshelfSourceRoutesAuthorizeBeforeRouteHandling(t *testing.T) {
	tests := []struct {
		name       string
		runtimeErr error
		cookie     bool
		role       auth.Role
		want       int
		calls      int
	}{
		{name: "auth unavailable", runtimeErr: auth.ErrRuntimeConfigUnavailable, want: http.StatusServiceUnavailable},
		{name: "anonymous", want: http.StatusUnauthorized},
		{name: "non admin", cookie: true, role: auth.RoleRequester, want: http.StatusForbidden},
		{name: "admin", cookie: true, role: auth.RoleAdmin, want: http.StatusOK, calls: 1},
	}
	for _, tt := range tests {
		for _, route := range []struct {
			name, method, target, body string
			adminStatus                int
		}{
			{"collection", http.MethodGet, "/api/v1/admin/inventory/audiobookshelf/sources", "", http.StatusOK},
			{"connection test", http.MethodPost, "/api/v1/admin/inventory/audiobookshelf/sources/connection-test", `{"endpointURL":"https://abs.example","credential":"candidate-secret"}`, http.StatusOK},
			{"item", http.MethodGet, "/api/v1/admin/inventory/audiobookshelf/sources/11111111-1111-4111-8111-111111111111", "", http.StatusOK},
			{"sync", http.MethodPost, "/api/v1/admin/inventory/audiobookshelf/sources/11111111-1111-4111-8111-111111111111/sync", `{}`, http.StatusAccepted},
		} {
			t.Run(tt.name+"/"+route.name, func(t *testing.T) {
				probe := &sourceProbeFake{}
				query := &sourceQueryFake{getView: sourceTestView()}
				sync := &manualSyncFake{result: inventory.ManualSyncRequestResult{Disposition: inventory.ManualSyncRecorded, PendingIntentID: "22222222-2222-4222-8222-222222222222"}}
				handler := sourceHandlerWithAuth(t, AudiobookshelfSourceOptions{Query: query, Probe: probe, ManualSync: sync}, tt.runtimeErr, tt.role)
				req := httptest.NewRequest(route.method, route.target, strings.NewReader(route.body))
				req.Header.Set("Origin", "https://requestarr.example")
				req.Header.Set("X-Requestarr-CSRF", "1")
				req.Header.Set("Content-Type", "application/json")
				if tt.cookie {
					req.AddCookie(&http.Cookie{Name: authSessionCookieName(false), Value: authTestSessionToken})
				}
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, req)
				want := tt.want
				if tt.role == auth.RoleAdmin && tt.runtimeErr == nil {
					want = route.adminStatus
				}
				calls := probe.calls + query.listCalls + query.getCalls + sync.calls
				if response.Code != want || calls != tt.calls || response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("X-Request-ID") == "" {
					t.Fatalf("status=%d calls=%d cache=%q request-id=%q", response.Code, calls, response.Header().Get("Cache-Control"), response.Header().Get("X-Request-ID"))
				}
			})
		}
	}
}

func TestAudiobookshelfSourceTypedErrorsAreSafe(t *testing.T) {
	secret := "do-not-return-this-secret"
	saveBody := `{"name":"Books","endpointURL":"https://abs.example","enabled":true,"selectedLibraries":["books"],"credentialMode":"preserve","expectedRevision":1}`
	for _, tt := range []struct {
		name, target, body, wantCode string
		wantStatus                   int
		options                      AudiobookshelfSourceOptions
	}{
		{"save not found", "/api/v1/admin/inventory/audiobookshelf/sources/11111111-1111-4111-8111-111111111111", saveBody, "source_not_found", http.StatusNotFound, AudiobookshelfSourceOptions{Save: &sourceSaveFake{err: fmt.Errorf("%s: %w", secret, inventory.ErrSourceNotFound)}}},
		{"save conflict", "/api/v1/admin/inventory/audiobookshelf/sources/11111111-1111-4111-8111-111111111111", saveBody, "source_configuration_conflict", http.StatusConflict, AudiobookshelfSourceOptions{Save: &sourceSaveFake{err: inventory.ErrSourceRevisionConflict}}},
		{"save invalid", "/api/v1/admin/inventory/audiobookshelf/sources/11111111-1111-4111-8111-111111111111", saveBody, "source_configuration_invalid", http.StatusUnprocessableEntity, AudiobookshelfSourceOptions{Save: &sourceSaveFake{err: inventory.ErrInvalidSourceSave}}},
		{"probe authentication", "/api/v1/admin/inventory/audiobookshelf/sources/connection-test", `{"endpointURL":"https://abs.example","credential":"do-not-return-this-secret"}`, "connection_test_authentication_failed", http.StatusUnauthorized, AudiobookshelfSourceOptions{Probe: &sourceProbeFake{err: fmt.Errorf("%s: %w", secret, inventory.ErrProbeAuthenticationFailed)}}},
		{"probe unreachable", "/api/v1/admin/inventory/audiobookshelf/sources/connection-test", `{"endpointURL":"https://abs.example","credential":"do-not-return-this-secret"}`, "connection_test_connection_failed", http.StatusBadGateway, AudiobookshelfSourceOptions{Probe: &sourceProbeFake{err: inventory.ErrProbeConnectionFailed}}},
		{"sync missing", "/api/v1/admin/inventory/audiobookshelf/sources/11111111-1111-4111-8111-111111111111/sync", `{}`, "source_not_found", http.StatusNotFound, AudiobookshelfSourceOptions{ManualSync: &manualSyncFake{err: fmt.Errorf("%s: %w", secret, inventory.ErrManualSyncSourceNotFound)}}},
		{"sync conflict", "/api/v1/admin/inventory/audiobookshelf/sources/11111111-1111-4111-8111-111111111111/sync", `{}`, "manual_sync_conflict", http.StatusConflict, AudiobookshelfSourceOptions{ManualSync: &manualSyncFake{err: inventory.ErrManualSyncRequestConflict}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			handler := sourceHandler(t, tt.options)
			method := http.MethodPost
			if strings.Contains(tt.target, "/sources/111") && !strings.HasSuffix(tt.target, "/sync") {
				method = http.MethodPut
			}
			response := serveAdminRequest(t, handler, method, tt.target, tt.body)
			if response.Code != tt.wantStatus || responseErrorCode(t, response) != tt.wantCode || strings.Contains(response.Body.String(), secret) || response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("X-Request-ID") == "" {
				t.Fatalf("status=%d code=%q body=%s", response.Code, responseErrorCode(t, response), response.Body.String())
			}
		})
	}
}

func sourceHandler(t *testing.T, options AudiobookshelfSourceOptions) http.Handler {
	return sourceHandlerWithAuth(t, options, nil, auth.RoleAdmin)
}

func sourceHandlerWithAuth(t *testing.T, options AudiobookshelfSourceOptions, runtimeErr error, role auth.Role) http.Handler {
	t.Helper()
	var key [32]byte
	codec := testLoginCodec(t)
	options.Auth = AuthOptions{Runtime: authRuntimeFake{config: testRuntimeConfig(), err: runtimeErr}, Browser: &adminBrowserFake{principal: auth.Principal{UserID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", Role: role}}, Cookies: codec, PublicOrigin: "https://requestarr.example:443"}
	if options.Cipher == nil {
		options.Cipher = secrets.New(key)
	}
	mux := http.NewServeMux()
	audiobookshelfSourceRoutes(mux, options)
	return requestID(mux)
}

func serveAdminRequest(t *testing.T, handler http.Handler, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	request.Header.Set("Origin", "https://requestarr.example")
	request.Header.Set("X-Requestarr-CSRF", "1")
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(&http.Cookie{Name: authSessionCookieName(false), Value: authTestSessionToken})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func testRuntimeConfig() auth.RuntimeConfig {
	return auth.RuntimeConfig{AuthenticationRevision: 1, Issuer: "https://issuer.example", ClientID: "requestarr", ClientType: "public", Scopes: []string{"openid"}, GroupsClaim: "groups", DefaultRole: auth.RoleViewer, ActivatedAt: time.Unix(1, 0)}
}

type sourceQueryFake struct {
	views     []inventory.SourceConfigurationView
	getView   inventory.SourceConfigurationView
	getErr    error
	listCalls int
	getCalls  int
}

func (f *sourceQueryFake) ListSourceConfigurations(context.Context) ([]inventory.SourceConfigurationView, error) {
	f.listCalls++
	return f.views, nil
}
func (f *sourceQueryFake) GetSourceConfiguration(context.Context, string) (inventory.SourceConfigurationView, error) {
	f.getCalls++
	if f.getErr != nil {
		return inventory.SourceConfigurationView{}, f.getErr
	}
	return f.getView, nil
}

func sourceTestView() inventory.SourceConfigurationView {
	return inventory.SourceConfigurationView{Configuration: inventory.SourceConfiguration{ID: "11111111-1111-4111-8111-111111111111", Kind: inventory.SourceAudiobookshelf}, EndpointURL: "https://abs.example", Revision: 1}
}

func sourceDownstreamCalls(probe *sourceProbeFake, query *sourceQueryFake, save *sourceSaveFake, sync *manualSyncFake) int {
	return probe.calls + query.listCalls + query.getCalls + save.calls + sync.calls
}

func sameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

type sourceProbeFake struct {
	calls   int
	request inventory.ConnectionProbeRequest
	result  inventory.ConnectionProbeResult
	err     error
}

func (f *sourceProbeFake) Probe(_ context.Context, request inventory.ConnectionProbeRequest) (inventory.ConnectionProbeResult, error) {
	f.calls++
	f.request = request
	return f.result, f.err
}

type manualSyncFake struct {
	calls   int
	request inventory.ManualSyncRequest
	result  inventory.ManualSyncRequestResult
	err     error
}

type sourceSaveFake struct {
	calls   int
	request inventory.SourceConfigurationSave
	result  inventory.SourceSaveResult
	err     error
}

func (f *sourceSaveFake) SaveSourceConfiguration(_ context.Context, request inventory.SourceConfigurationSave) (inventory.SourceSaveResult, error) {
	f.calls++
	f.request = request
	return f.result, f.err
}

func fixedIDGenerator(values ...string) func() (string, error) {
	index := 0
	return func() (string, error) {
		if index >= len(values) {
			return "", errors.New("no identifier available")
		}
		value := values[index]
		index++
		return value, nil
	}
}

func (f *manualSyncFake) Request(_ context.Context, request inventory.ManualSyncRequest) (inventory.ManualSyncRequestResult, error) {
	f.calls++
	f.request = request
	return f.result, f.err
}

func responseErrorCode(t *testing.T, response *httptest.ResponseRecorder) string {
	t.Helper()
	var envelope apiErrorEnvelope
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope.Error.Code
}

var _ = json.Valid
