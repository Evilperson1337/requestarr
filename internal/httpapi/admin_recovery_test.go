package httpapi

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/evilperson1337/requestarr/internal/auth"
	"github.com/evilperson1337/requestarr/internal/buildinfo"
)

func TestAdministratorRecoveryHasNoPublicSetupRoutes(t *testing.T) {
	d := testDeps(testLogger(), buildinfo.Info{}, fakeReadiness{}, nil, nil, nil, &auth.Handler{Secret: "test-secret"})
	_, rt, err := buildRouter(d)
	if err != nil {
		t.Fatal(err)
	}
	for _, policy := range rt.Policies() {
		if strings.Contains(policy.Pattern, "/setup/") {
			t.Errorf("obsolete setup route registered: %s", policy.Pattern)
		}
	}
	for op := range specOperations(t) {
		if strings.Contains(op, "/setup/") {
			t.Errorf("obsolete setup operation documented: %s", op)
		}
	}
}

func TestCurrentUserResponseContractPerRole(t *testing.T) {
	for _, role := range []string{"unauthenticated", "viewer", "requester", "admin"} {
		t.Run(role, func(t *testing.T) {
			h := localRouter(t, auth.User{ID: matrixUserID, Role: role, Issuer: "private-issuer-canary", Subject: "private-subject-canary"}, &localFake{})
			w := localCall(h, "GET", "/auth/me", "", role != "unauthenticated")
			if role == "unauthenticated" {
				assertErrorEnvelope(t, w, 401, "authentication_required")
				return
			}
			var body map[string]json.RawMessage
			if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &body) != nil || len(body) != 4 {
				t.Fatal("current-user response does not match the four-field session contract")
			}
			for _, key := range []string{"user", "csrfToken", "mustChangePassword", "localAdministrator"} {
				if _, ok := body[key]; !ok {
					t.Errorf("missing session field %s", key)
				}
			}
			if w.Header().Get("Cache-Control") != "no-store" || strings.Contains(w.Body.String(), "private-issuer-canary") || strings.Contains(w.Body.String(), "private-subject-canary") {
				t.Fatal("current-user response leaked OIDC identity or omitted cache protection")
			}
		})
	}
}
