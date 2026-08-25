package httpapi

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"git.example.com/evilperson1337/Requestarr/internal/buildinfo"
	"git.example.com/evilperson1337/Requestarr/internal/setup"
)

type workflowErrorPersistence struct {
	err error
}

func (p workflowErrorPersistence) ReadWorkflow(context.Context, setup.Digest) (setup.WorkflowResult, error) {
	return setup.WorkflowResult{}, p.err
}

func (p workflowErrorPersistence) SaveGeneral(context.Context, setup.Digest, setup.General) (setup.WorkflowResult, error) {
	return setup.WorkflowResult{}, p.err
}

func (p workflowErrorPersistence) SaveAuthentication(context.Context, setup.Digest, setup.Authentication, setup.SecretOperation) (setup.WorkflowResult, error) {
	return setup.WorkflowResult{}, p.err
}

type setupWorkflowFake struct {
	mu          sync.Mutex
	stateResult setup.WorkflowResult
	putResult   setup.WorkflowResult
	session     string
	general     setup.General
	stateCalls  int
	putCalls    int
	authCalls   int
	auth        setup.Authentication
	secretKind  setup.SecretOperationKind
}

type authenticationValidatorFake struct {
	mu       sync.Mutex
	result   setup.AuthenticationValidationResult
	session  string
	revision int64
	calls    int
}

type authenticationActivatorFake struct {
	result  setup.ActivationResult
	session string
	calls   int
}

func TestSetupAuthenticationActivationContract(t *testing.T) {
	activator := &authenticationActivatorFake{result: setup.ActivationResult{Outcome: setup.ActivationSucceeded, Revision: 7}}
	options := SetupSessionOptions{AuthenticationActivator: activator, PublicOrigin: "https://requestarr.example:443", SecureCookie: false}
	handler := noStore(setupAuthenticationActivationHandler(options))
	request := httptest.NewRequest(http.MethodPost, "/api/v1/setup/activate-auth", strings.NewReader(`{}`))
	request.Header.Set("Origin", "https://requestarr.example")
	request.Header.Set("X-Requestarr-CSRF", "1")
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(&http.Cookie{Name: "requestarr_setup_dev", Value: strings.Repeat("A", 43)})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"setup_complete":false`) || activator.calls != 1 {
		t.Fatalf("activation response=%d body=%s calls=%d", recorder.Code, recorder.Body.String(), activator.calls)
	}
	if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control=%q", got)
	}
}

func TestSetupAuthenticationActivationFailurePathsAndPrecedence(t *testing.T) {
	valid := func() *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/setup/activate-auth", strings.NewReader(`{}`))
		r.Header.Set("Origin", "https://requestarr.example")
		r.Header.Set("X-Requestarr-CSRF", "1")
		r.Header.Set("Content-Type", "application/json")
		r.AddCookie(&http.Cookie{Name: "requestarr_setup_dev", Value: strings.Repeat("A", 43)})
		return r
	}
	tests := []struct {
		name       string
		mutate     func(*http.Request)
		result     setup.ActivationResult
		wantStatus int
		wantCode   string
		wantCalls  int
	}{
		{name: "method precedes origin", mutate: func(r *http.Request) { r.Method = http.MethodGet; r.Header.Del("Origin") }, wantStatus: http.StatusMethodNotAllowed, wantCalls: 0},
		{name: "origin precedes csrf", mutate: func(r *http.Request) {
			r.Header.Set("Origin", "https://other.example")
			r.Header.Del("X-Requestarr-CSRF")
		}, wantStatus: http.StatusForbidden, wantCode: "setup_origin_forbidden", wantCalls: 0},
		{name: "csrf precedes content type", mutate: func(r *http.Request) { r.Header.Del("X-Requestarr-CSRF"); r.Header.Set("Content-Type", "text/plain") }, wantStatus: http.StatusForbidden, wantCode: "setup_csrf_forbidden", wantCalls: 0},
		{name: "content type precedes body", mutate: func(r *http.Request) {
			r.Header.Set("Content-Type", "text/plain")
			r.Body = io.NopCloser(strings.NewReader("not json"))
		}, wantStatus: http.StatusBadRequest, wantCode: "invalid_request", wantCalls: 0},
		{name: "invalid body precedes cookie", mutate: func(r *http.Request) { r.Body = io.NopCloser(strings.NewReader("{")); r.Header.Del("Cookie") }, wantStatus: http.StatusBadRequest, wantCode: "invalid_request", wantCalls: 0},
		{name: "missing cookie", mutate: func(r *http.Request) { r.Header.Del("Cookie") }, wantStatus: http.StatusUnauthorized, wantCode: "setup_session_required", wantCalls: 0},
		{name: "duplicate cookie", mutate: func(r *http.Request) {
			r.AddCookie(&http.Cookie{Name: "requestarr_setup_dev", Value: strings.Repeat("B", 43)})
		}, wantStatus: http.StatusUnauthorized, wantCode: "setup_session_required", wantCalls: 0},
		{name: "required", result: setup.ActivationResult{Outcome: setup.ActivationRequired}, wantStatus: http.StatusUnprocessableEntity, wantCode: "setup_authentication_activation_required", wantCalls: 1},
		{name: "stale", result: setup.ActivationResult{Outcome: setup.ActivationStale}, wantStatus: http.StatusConflict, wantCode: "setup_authentication_validation_stale", wantCalls: 1},
		{name: "already active", result: setup.ActivationResult{Outcome: setup.ActivationAlreadyActive}, wantStatus: http.StatusConflict, wantCode: "setup_authentication_already_activated", wantCalls: 1},
		{name: "locked", result: setup.ActivationResult{Outcome: setup.ActivationLocked}, wantStatus: http.StatusLocked, wantCode: "setup_locked", wantCalls: 1},
		{name: "unavailable", result: setup.ActivationResult{Outcome: setup.ActivationUnavailable}, wantStatus: http.StatusServiceUnavailable, wantCode: "setup_unavailable", wantCalls: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &authenticationActivatorFake{result: tt.result}
			r := valid()
			if tt.mutate != nil {
				tt.mutate(r)
			}
			w := httptest.NewRecorder()
			noStore(setupAuthenticationActivationHandler(SetupSessionOptions{AuthenticationActivator: fake, PublicOrigin: "https://requestarr.example:443", SecureCookie: false})).ServeHTTP(w, r)
			if w.Code != tt.wantStatus || fake.calls != tt.wantCalls || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("status=%d calls=%d cache=%q body=%s", w.Code, fake.calls, w.Header().Get("Cache-Control"), w.Body.String())
			}
			if tt.wantCode != "" && !strings.Contains(w.Body.String(), `"code":"`+tt.wantCode+`"`) {
				t.Fatalf("body=%s, want code %q", w.Body.String(), tt.wantCode)
			}
			if w.Code == http.StatusMethodNotAllowed && w.Header().Get("Allow") != http.MethodPost {
				t.Fatalf("Allow=%q", w.Header().Get("Allow"))
			}
		})
	}
}

func (f *authenticationActivatorFake) Activate(_ context.Context, session string) setup.ActivationResult {
	f.calls++
	f.session = session
	return f.result
}

func (f *authenticationValidatorFake) Validate(_ context.Context, session string, revision int64) setup.AuthenticationValidationResult {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.session, f.revision = session, revision
	return f.result
}

func (f *setupWorkflowFake) State(_ context.Context, session string) setup.WorkflowResult {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stateCalls++
	f.session = session
	return f.stateResult
}

func (f *setupWorkflowFake) PutGeneral(_ context.Context, session string, general setup.General) setup.WorkflowResult {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.putCalls++
	f.session, f.general = session, general
	return f.putResult
}

func (f *setupWorkflowFake) PutAuthentication(_ context.Context, session string, authentication setup.Authentication, operation setup.SecretOperation) setup.WorkflowResult {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.session = session
	f.authCalls++
	f.auth = authentication
	f.secretKind = operation.Kind()
	return f.putResult
}

func (f *setupWorkflowFake) calls() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.stateCalls, f.putCalls
}

func TestSetupAuthenticationContract(t *testing.T) {
	base := `{"issuer":"https://id.example.com/realms/main","client_id":"requestarr","client_type":"confidential","scopes":["openid"],"groups_claim":"groups","default_role":"viewer","group_role_mappings":[],"client_secret_operation":"replace","client_secret":"distinctive-secret"}`
	request := func(body string) *http.Request {
		r := httptest.NewRequest(http.MethodPut, "/api/v1/setup/authentication", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", "https://example.com")
		r.Header.Set("X-Requestarr-CSRF", "1")
		r.AddCookie(&http.Cookie{Name: "__Host-requestarr_setup", Value: httpSessionValue()})
		return r
	}
	fake := &setupWorkflowFake{putResult: setup.WorkflowResult{Outcome: setup.WorkflowAvailable, State: setup.WorkflowState{Current: setup.StepProviders, ClientSecretConfigured: true}}}
	response := httptest.NewRecorder()
	handlerWithSetupWorkflow(fake).ServeHTTP(response, request(base))
	if response.Code != http.StatusOK || fake.authCalls != 1 || fake.auth.ClientType != setup.ClientTypeConfidential || fake.secretKind != setup.SecretReplace {
		t.Fatalf("valid Authentication result status=%d calls=%d auth=%#v operation=%d", response.Code, fake.authCalls, fake.auth, fake.secretKind)
	}
	if strings.Contains(response.Body.String(), "distinctive-secret") || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("Authentication response leaked secret or omitted no-store")
	}

	invalid := []string{
		strings.Replace(base, `"client_type":"confidential"`, `"client_type":"public"`, 1),
		strings.Replace(strings.Replace(base, `"client_type":"confidential"`, `"client_type":"public"`, 1), `"client_secret_operation":"replace","client_secret":"distinctive-secret"`, `"client_secret_operation":"preserve"`, 1),
		strings.Replace(base, `"client_secret_operation":"replace","client_secret":"distinctive-secret"`, `"client_secret_operation":"clear"`, 1),
		strings.Replace(base, `"issuer":"https://id.example.com/realms/main"`, `"issuer":"https://id.example.com","issuer":"https://evil.example.com"`, 1),
		strings.Replace(base, `"group_role_mappings":[]`, `"group_role_mappings":[{"group":"users","group":"admins","role":"viewer"}]`, 1),
	}
	for _, body := range invalid {
		got := httptest.NewRecorder()
		handlerWithSetupWorkflow(&setupWorkflowFake{}).ServeHTTP(got, request(body))
		if got.Code != http.StatusBadRequest && got.Code != http.StatusUnprocessableEntity {
			t.Errorf("invalid combination status=%d body=%s", got.Code, got.Body.String())
		}
		if strings.Contains(got.Body.String(), "distinctive-secret") {
			t.Fatal("error response leaked secret")
		}
	}
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPatch, http.MethodDelete, http.MethodOptions} {
		got := httptest.NewRecorder()
		handlerWithSetupWorkflow(fake).ServeHTTP(got, httptest.NewRequest(method, "/api/v1/setup/authentication", nil))
		if got.Code != http.StatusMethodNotAllowed || got.Header().Get("Allow") != http.MethodPut {
			t.Errorf("%s status=%d Allow=%q", method, got.Code, got.Header().Get("Allow"))
		}
	}
}

func TestSetupAuthenticationValidationContractAndPrecedence(t *testing.T) {
	validRequest := func(method, body string) *http.Request {
		request := httptest.NewRequest(method, "/api/v1/setup/authentication/validate", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", "https://example.com")
		request.Header.Set("X-Requestarr-CSRF", "1")
		request.AddCookie(&http.Cookie{Name: "__Host-requestarr_setup", Value: httpSessionValue()})
		return request
	}
	tests := []struct {
		name   string
		method string
		body   string
		mutate func(*http.Request)
		status int
		code   string
	}{
		{name: "method", method: http.MethodPut, body: `{`, mutate: func(r *http.Request) {
			r.Header.Del("Origin")
			r.Header.Del("X-Requestarr-CSRF")
			r.Header.Del("Cookie")
		}, status: 405, code: "method_not_allowed"},
		{name: "origin", method: http.MethodPost, body: `{`, mutate: func(r *http.Request) {
			r.Header.Del("Origin")
			r.Header.Del("X-Requestarr-CSRF")
			r.Header.Del("Cookie")
		}, status: 403, code: "setup_origin_forbidden"},
		{name: "csrf", method: http.MethodPost, body: `{`, mutate: func(r *http.Request) { r.Header.Del("X-Requestarr-CSRF"); r.Header.Del("Cookie") }, status: 403, code: "setup_csrf_forbidden"},
		{name: "content type", method: http.MethodPost, body: `{"authentication_revision":1}`, mutate: func(r *http.Request) { r.Header.Set("Content-Type", "text/plain"); r.Header.Del("Cookie") }, status: 400, code: "invalid_request"},
		{name: "body", method: http.MethodPost, body: `{`, mutate: func(r *http.Request) { r.Header.Del("Cookie") }, status: 400, code: "invalid_request"},
		{name: "cookie", method: http.MethodPost, body: `{"authentication_revision":1}`, mutate: func(r *http.Request) { r.Header.Del("Cookie") }, status: 401, code: "setup_session_required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validator := &authenticationValidatorFake{}
			handler := handlerWithAuthenticationValidator(validator, nil)
			request := validRequest(tt.method, tt.body)
			tt.mutate(request)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			assertAPIError(t, response, tt.status, tt.code)
			if validator.calls != 0 {
				t.Fatalf("validator called %d times", validator.calls)
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("validation response omitted no-store")
			}
		})
	}

	for _, body := range []string{
		`{"authentication_revision":0}`,
		`{"authentication_revision":-1}`,
		`{"authentication_revision":1.0}`,
		`{"authentication_revision":01}`,
		`{"authentication_revision":"1"}`,
		`{"authentication_revision":1,"extra":"distinctive-secret"}`,
		`{"authentication_revision":1,"authentication_revision":2}`,
	} {
		response := httptest.NewRecorder()
		handlerWithAuthenticationValidator(&authenticationValidatorFake{}, nil).ServeHTTP(response, validRequest(http.MethodPost, body))
		assertAPIError(t, response, http.StatusBadRequest, "invalid_request")
		if strings.Contains(response.Body.String(), "distinctive-secret") {
			t.Fatal("invalid response exposed request value")
		}
	}

	validatedAt := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	validator := &authenticationValidatorFake{result: setup.AuthenticationValidationResult{Outcome: setup.AuthenticationValidationSucceeded, Revision: 7,
		Evidence: &setup.AuthenticationValidationEvidence{AuthenticationRevision: 7, PolicyVersion: 1, ValidatedAt: validatedAt}}}
	response := httptest.NewRecorder()
	handlerWithAuthenticationValidator(validator, nil).ServeHTTP(response, validRequest(http.MethodPost, `{"authentication_revision":7}`))
	if response.Code != http.StatusOK || validator.calls != 1 || validator.revision != 7 || validator.session != httpSessionValue() {
		t.Fatalf("status=%d calls=%d revision=%d session=%q", response.Code, validator.calls, validator.revision, validator.session)
	}
	if !strings.Contains(response.Body.String(), `"authentication_revision":7`) || strings.Contains(response.Body.String(), httpSessionValue()) {
		t.Fatalf("response = %s", response.Body.String())
	}
}

func TestSetupAuthenticationValidationOutcomeMappingAndRedaction(t *testing.T) {
	tests := []struct {
		outcome setup.AuthenticationValidationOutcome
		status  int
		code    string
		retry   time.Duration
	}{
		{setup.AuthenticationValidationCompleted, 409, "setup_completed", 0},
		{setup.AuthenticationValidationStale, 409, "setup_authentication_validation_stale", 0},
		{setup.AuthenticationValidationRequired, 422, "setup_authentication_validation_required", 0},
		{setup.AuthenticationValidationDestination, 422, "setup_authentication_destination_invalid", 0},
		{setup.AuthenticationValidationUnreachable, 422, "setup_authentication_unreachable", 0},
		{setup.AuthenticationValidationRateLimited, 429, "setup_authentication_validation_rate_limited", 999 * time.Second},
		{setup.AuthenticationValidationUnavailable, 503, "setup_unavailable", 0},
	}
	for _, tt := range tests {
		validator := &authenticationValidatorFake{result: setup.AuthenticationValidationResult{Outcome: tt.outcome, RetryAfter: tt.retry}}
		request := httptest.NewRequest(http.MethodPost, "/api/v1/setup/authentication/validate?issuer=https://distinctive.invalid", strings.NewReader(`{"authentication_revision":1}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", "https://example.com")
		request.Header.Set("X-Requestarr-CSRF", "1")
		request.Header.Set("Authorization", "Bearer distinctive-authorization")
		request.AddCookie(&http.Cookie{Name: "__Host-requestarr_setup", Value: httpSessionValue()})
		var logs bytes.Buffer
		response := httptest.NewRecorder()
		handlerWithAuthenticationValidator(validator, &logs).ServeHTTP(response, request)
		assertAPIError(t, response, tt.status, tt.code)
		if tt.status == 429 && response.Header().Get("Retry-After") != "300" {
			t.Fatalf("Retry-After = %q", response.Header().Get("Retry-After"))
		}
		for _, forbidden := range []string{"distinctive.invalid", "distinctive-authorization", httpSessionValue(), "Authorization", "Cookie", "issuer="} {
			if strings.Contains(response.Body.String(), forbidden) || strings.Contains(logs.String(), forbidden) {
				t.Fatalf("validation output exposed %q: body=%s logs=%s", forbidden, response.Body.String(), logs.String())
			}
		}
	}
}

func TestSetupStateRequiresSessionButNotOriginOrCSRF(t *testing.T) {
	fake := &setupWorkflowFake{stateResult: setup.WorkflowResult{Outcome: setup.WorkflowAvailable, State: setup.WorkflowState{
		Current: setup.StepAuthentication,
		General: &setup.General{InstanceName: "Requestarr", PublicURL: "https://example.com", Timezone: "UTC"},
	}}}
	handler := handlerWithSetupWorkflow(fake)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/setup/state", nil)
	request.AddCookie(&http.Cookie{Name: "__Host-requestarr_setup", Value: httpSessionValue()})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	assertJSONResponse(t, response, http.StatusOK, `{"current_step":"authentication","general":{"instance_name":"Requestarr","public_url":"https://example.com","timezone":"UTC"}}`)
	if fake.session != httpSessionValue() {
		t.Fatal("workflow did not receive cookie session")
	}

	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/api/v1/setup/state", nil))
	assertAPIError(t, unauthorized, http.StatusUnauthorized, "setup_session_required")
}

func TestSetupGeneralSecurityParsingAndOutcome(t *testing.T) {
	fake := &setupWorkflowFake{putResult: setup.WorkflowResult{Outcome: setup.WorkflowAvailable, State: setup.WorkflowState{
		Current: setup.StepAuthentication,
		General: &setup.General{InstanceName: "Requestarr", PublicURL: "https://example.com", Timezone: "UTC"},
	}}}
	handler := handlerWithSetupWorkflow(fake)
	valid := func(body string) *http.Request {
		request := httptest.NewRequest(http.MethodPut, "/api/v1/setup/general", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", "https://example.com")
		request.Header.Set("X-Requestarr-CSRF", "1")
		request.AddCookie(&http.Cookie{Name: "__Host-requestarr_setup", Value: httpSessionValue()})
		return request
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, valid(`{"instance_name":"Requestarr","public_url":"https://example.com","timezone":"UTC"}`))
	assertJSONResponse(t, response, http.StatusOK, `{"current_step":"authentication","general":{"instance_name":"Requestarr","public_url":"https://example.com","timezone":"UTC"}}`)

	for name, testCase := range map[string]struct {
		mutate func(*http.Request)
		code   string
	}{
		"origin": {func(r *http.Request) { r.Header.Del("Origin") }, "setup_origin_forbidden"},
		"csrf":   {func(r *http.Request) { r.Header.Del("X-Requestarr-CSRF") }, "setup_csrf_forbidden"},
		"cookie": {func(r *http.Request) { r.Header.Del("Cookie") }, "setup_session_required"},
	} {
		t.Run(name, func(t *testing.T) {
			request := valid(`{"instance_name":"Requestarr","public_url":"https://example.com","timezone":"UTC"}`)
			testCase.mutate(request)
			got := httptest.NewRecorder()
			handler.ServeHTTP(got, request)
			assertAPIError(t, got, map[string]int{"origin": 403, "csrf": 403, "cookie": 401}[name], testCase.code)
		})
	}
	bad := httptest.NewRecorder()
	handler.ServeHTTP(bad, valid(`{"instance_name":"Requestarr","public_url":"https://example.com","timezone":"UTC","secret":"do-not-store"}`))
	assertAPIError(t, bad, http.StatusBadRequest, "invalid_request")
	for _, body := range []string{
		`{"instance_name":"Requestarr","public_url":"https://example.com"}`,
		`{"instance_name":"Requestarr","instance_name":"Other","public_url":"https://example.com","timezone":"UTC"}`,
		`{"instance_name":1,"public_url":"https://example.com","timezone":"UTC"}`,
	} {
		got := httptest.NewRecorder()
		handler.ServeHTTP(got, valid(body))
		assertAPIError(t, got, http.StatusBadRequest, "invalid_request")
	}
}

func TestSetupGeneralMutationPrecedence(t *testing.T) {
	validBody := `{"instance_name":"Requestarr","public_url":"https://example.com","timezone":"UTC"}`
	tests := []struct {
		name   string
		method string
		body   string
		mutate func(*http.Request)
		status int
		code   string
	}{
		{name: "method before all", method: http.MethodPost, body: `{`, mutate: func(r *http.Request) {
			r.Header.Del("Origin")
			r.Header.Del("X-Requestarr-CSRF")
			r.Header.Del("Cookie")
		}, status: 405, code: "method_not_allowed"},
		{name: "origin before CSRF body and cookie", method: http.MethodPut, body: `{`, mutate: func(r *http.Request) {
			r.Header.Del("Origin")
			r.Header.Del("X-Requestarr-CSRF")
			r.Header.Del("Cookie")
		}, status: 403, code: "setup_origin_forbidden"},
		{name: "CSRF before body and cookie", method: http.MethodPut, body: `{`, mutate: func(r *http.Request) { r.Header.Del("X-Requestarr-CSRF"); r.Header.Del("Cookie") }, status: 403, code: "setup_csrf_forbidden"},
		{name: "content type before cookie", method: http.MethodPut, body: validBody, mutate: func(r *http.Request) { r.Header.Set("Content-Type", "text/plain"); r.Header.Del("Cookie") }, status: 400, code: "invalid_request"},
		{name: "JSON shape before cookie", method: http.MethodPut, body: `{`, mutate: func(r *http.Request) { r.Header.Del("Cookie") }, status: 400, code: "invalid_request"},
		{name: "typed validation before cookie", method: http.MethodPut, body: `{"instance_name":"","public_url":"bad","timezone":"Local"}`, mutate: func(r *http.Request) { r.Header.Del("Cookie") }, status: 422, code: "setup_general_invalid"},
		{name: "cookie after valid request", method: http.MethodPut, body: validBody, mutate: func(r *http.Request) { r.Header.Del("Cookie") }, status: 401, code: "setup_session_required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &setupWorkflowFake{}
			handler := handlerWithSetupWorkflow(fake)
			request := httptest.NewRequest(tt.method, "/api/v1/setup/general", strings.NewReader(tt.body))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Origin", "https://example.com")
			request.Header.Set("X-Requestarr-CSRF", "1")
			request.AddCookie(&http.Cookie{Name: "__Host-requestarr_setup", Value: httpSessionValue()})
			tt.mutate(request)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			assertAPIError(t, response, tt.status, tt.code)
			if _, calls := fake.calls(); calls != 0 {
				t.Fatalf("workflow invoked %d times for rejected request", calls)
			}
		})
	}
}

func TestSetupWorkflowRejectsAmbiguousSecurityHeadersAndCookies(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*http.Request)
		status int
		code   string
	}{
		{name: "repeated origin", mutate: func(r *http.Request) { r.Header.Add("Origin", "https://example.com") }, status: 403, code: "setup_origin_forbidden"},
		{name: "comma joined origin", mutate: func(r *http.Request) { r.Header.Set("Origin", "https://example.com, https://example.com") }, status: 403, code: "setup_origin_forbidden"},
		{name: "repeated CSRF", mutate: func(r *http.Request) { r.Header.Add("X-Requestarr-CSRF", "1") }, status: 403, code: "setup_csrf_forbidden"},
		{name: "comma joined CSRF", mutate: func(r *http.Request) { r.Header.Set("X-Requestarr-CSRF", "1, 1") }, status: 403, code: "setup_csrf_forbidden"},
		{name: "duplicate selected cookie", mutate: func(r *http.Request) {
			r.AddCookie(&http.Cookie{Name: "__Host-requestarr_setup", Value: httpSessionValue()})
		}, status: 401, code: "setup_session_required"},
		{name: "wrong environment cookie", mutate: func(r *http.Request) {
			r.Header.Del("Cookie")
			r.AddCookie(&http.Cookie{Name: "requestarr_setup_dev", Value: httpSessionValue()})
		}, status: 401, code: "setup_session_required"},
		{name: "both environment names", mutate: func(r *http.Request) {
			r.AddCookie(&http.Cookie{Name: "requestarr_setup_dev", Value: "do-not-disclose"})
		}, status: 401, code: "setup_session_required"},
		{name: "malformed cookie", mutate: func(r *http.Request) { r.Header.Set("Cookie", "__Host-requestarr_setup") }, status: 401, code: "setup_session_required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &setupWorkflowFake{putResult: setup.WorkflowResult{Outcome: setup.WorkflowAvailable, State: setup.WorkflowState{Current: setup.StepAuthentication}}}
			request := httptest.NewRequest(http.MethodPut, "/api/v1/setup/general", strings.NewReader(`{"instance_name":"Requestarr","public_url":"https://example.com","timezone":"UTC"}`))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Origin", "https://example.com")
			request.Header.Set("X-Requestarr-CSRF", "1")
			request.AddCookie(&http.Cookie{Name: "__Host-requestarr_setup", Value: httpSessionValue()})
			tt.mutate(request)
			response := httptest.NewRecorder()
			handlerWithSetupWorkflow(fake).ServeHTTP(response, request)
			if response.Code != tt.status {
				t.Fatalf("status = %d, want %d; body=%s", response.Code, tt.status, response.Body.String())
			}
			if tt.code != "" {
				assertAPIError(t, response, tt.status, tt.code)
				if _, calls := fake.calls(); calls != 0 {
					t.Fatalf("workflow invoked %d times", calls)
				}
			}
			if strings.Contains(response.Body.String(), "do-not-disclose") || strings.Contains(response.Body.String(), httpSessionValue()) {
				t.Fatal("response disclosed cookie material")
			}
		})
	}
}

func TestSetupWorkflowMethodAndHeadContracts(t *testing.T) {
	fake := &setupWorkflowFake{stateResult: setup.WorkflowResult{Outcome: setup.WorkflowAvailable, State: setup.WorkflowState{Current: setup.StepGeneral}}}
	handler := handlerWithSetupWorkflow(fake)
	for _, method := range []string{http.MethodOptions, http.MethodPost} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(method, "/api/v1/setup/general", nil))
		assertAPIError(t, response, http.StatusMethodNotAllowed, "method_not_allowed")
		if response.Header().Get("Allow") != http.MethodPut {
			t.Fatalf("Allow = %q", response.Header().Get("Allow"))
		}
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	request, err := http.NewRequest(http.MethodHead, server.URL+"/api/v1/setup/state", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.AddCookie(&http.Cookie{Name: "__Host-requestarr_setup", Value: httpSessionValue()})
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.ContentLength <= 0 {
		t.Fatalf("HEAD status=%d content-length=%d", response.StatusCode, response.ContentLength)
	}
	if body := make([]byte, 1); func() int { n, _ := response.Body.Read(body); return n }() != 0 {
		t.Fatal("HEAD response contained a body")
	}
}

func TestSetupWorkflowAccessLogRedactsRequestAndAdapterValues(t *testing.T) {
	const (
		originSecret        = "origin-redaction.invalid"
		csrfSecret          = "csrf-redaction-value"
		bodySecret          = "body-redaction-value"
		querySecret         = "query-redaction-value"
		authorizationSecret = "authorization-redaction-value"
		adapterSecret       = "adapter-redaction-value"
	)
	var token [32]byte
	for i := range token {
		token[i] = byte(i + 1)
	}
	workflow := setup.NewWorkflowService(workflowErrorPersistence{err: errors.New(adapterSecret)}, token, true)
	var logs bytes.Buffer
	handler := New(slog.New(slog.NewJSONHandler(&logs, nil)), buildinfo.Info{}, nil, successfulSetupStatus(false), SetupSessionOptions{
		Workflow: workflow, PublicOrigin: "https://" + originSecret + ":443", SecureCookie: true,
	})
	cookieValue := strings.Repeat("Q", 42) + "g"
	request := httptest.NewRequest(http.MethodPut, "/api/v1/setup/general?probe="+querySecret,
		strings.NewReader(`{"instance_name":"`+bodySecret+`","public_url":"https://example.com","timezone":"UTC"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://"+originSecret)
	request.Header.Set("X-Requestarr-CSRF", "1")
	request.Header.Set("Authorization", "Bearer "+authorizationSecret)
	request.AddCookie(&http.Cookie{Name: "__Host-requestarr_setup", Value: cookieValue})
	handler.ServeHTTP(httptest.NewRecorder(), request)

	// Exercise the rejected-CSRF path separately because the accepted wire value
	// is deliberately fixed to "1" and therefore cannot carry a distinctive value.
	csrfRequest := httptest.NewRequest(http.MethodPut, "/api/v1/setup/general", nil)
	csrfRequest.Header.Set("Origin", "https://"+originSecret)
	csrfRequest.Header.Set("X-Requestarr-CSRF", csrfSecret)
	handler.ServeHTTP(httptest.NewRecorder(), csrfRequest)

	line := logs.String()
	if got := strings.Count(line, `"route":"/api/v1/setup/general"`); got != 2 {
		t.Fatalf("workflow access logs contain %d route-pattern records, want 2: %s", got, line)
	}
	for _, secret := range []string{
		originSecret, csrfSecret, bodySecret, querySecret, authorizationSecret, adapterSecret,
		cookieValue, "Authorization", "Cookie", "Origin", "X-Requestarr-CSRF", "probe=",
	} {
		if strings.Contains(line, secret) {
			t.Errorf("workflow access log contains secret-bearing value %q: %s", secret, line)
		}
	}
}

func handlerWithSetupWorkflow(workflow SetupWorkflow) http.Handler {
	return New(nil, buildinfo.Info{}, nil, successfulSetupStatus(false), SetupSessionOptions{
		Workflow: workflow, PublicOrigin: "https://example.com:443", SecureCookie: true,
	})
}

func handlerWithAuthenticationValidator(validator SetupAuthenticationValidator, logs *bytes.Buffer) http.Handler {
	logger := slog.Default()
	if logs != nil {
		logger = slog.New(slog.NewJSONHandler(logs, nil))
	}
	return New(logger, buildinfo.Info{}, nil, successfulSetupStatus(false), SetupSessionOptions{
		AuthenticationValidator: validator, PublicOrigin: "https://example.com:443", SecureCookie: true,
	})
}
