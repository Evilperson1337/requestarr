package httpapi

import (
	"reflect"
	"testing"

	"github.com/evilperson1337/requestarr/internal/auth"
	"github.com/evilperson1337/requestarr/internal/buildinfo"
)

// These are the superuser-only routes, spelled out so that adding or dropping
// one is a decision someone reads. Everything else under /admin stays with
// administrators (D035, four-roles ticket 03).
func TestSuperuserOnlyRoutesAreExactlyThese(t *testing.T) {
	_, rt := newRouter(testLogger(), buildinfo.Info{}, fakeReadiness{}, nil, nil, nil, &auth.Handler{Secret: "test-secret"})
	want := []string{
		"DELETE /admin/diagnostics-tokens/{id}",
		"DELETE /admin/library-connections/{slug}",
		"DELETE /admin/secrets/{name}",
		"DELETE /admin/settings/authentication",
		"GET /admin/checklist",
		"GET /admin/diagnostics-tokens",
		"GET /admin/library-connections",
		"GET /admin/secrets",
		"GET /admin/settings/authentication",
		"GET /admin/settings/authentication/local-administrator",
		"GET /admin/settings/igdb",
		"POST /admin/checklist/dismiss",
		"POST /admin/diagnostics-tokens",
		"POST /admin/library-connections/{slug}/test",
		"POST /admin/settings/authentication/local-administrator",
		"POST /admin/settings/authentication/test",
		"PUT /admin/library-connections/{slug}",
		"PUT /admin/secrets/{name}",
		"PUT /admin/settings/authentication",
		"PUT /admin/settings/authentication/enabled",
		"PUT /admin/settings/authentication/local-administrator/disabled",
		"PUT /admin/settings/igdb",
	}
	var got []string
	for _, p := range rt.Policies() {
		if p.Access == accessSuperuser {
			got = append(got, p.Pattern)
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("superuser-only routes:\n got %q\nwant %q", got, want)
	}
}

// An admin keeps the day-to-day surface: approvals, issues, matches, status,
// activity (read only), the approval policy, the default role and the Users list.
func TestAdminKeepsTheDayToDayRoutes(t *testing.T) {
	_, rt := newRouter(testLogger(), buildinfo.Info{}, fakeReadiness{}, nil, nil, nil, &auth.Handler{Secret: "test-secret"})
	byPattern := map[string]routePolicy{}
	for _, p := range rt.Policies() {
		byPattern[p.Pattern] = p
	}
	for _, pattern := range []string{
		"GET /admin/users", "PUT /admin/users/{userID}/role",
		"GET /admin/settings/people", "PUT /admin/settings/people",
		"GET /admin/settings/approvals", "PUT /admin/settings/approvals",
		"GET /admin/status", "GET /admin/job-runs",
		"GET /admin/match-suggestions", "POST /admin/match-suggestions/{suggestionID}/accept",
		"POST /admin/match-suggestions/{suggestionID}/dismiss",
		"POST /admin/sources/{source}/sync", "GET /requests/summary", "POST /issues/{id}/status",
	} {
		if p, ok := byPattern[pattern]; !ok || p.Access != accessAdmin {
			t.Errorf("%s = %+v (registered %v), want admin access", pattern, p, ok)
		}
	}
}
