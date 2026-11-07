package httpapi

import (
	"testing"

	"github.com/evilperson1337/requestarr/internal/auth"
	"github.com/evilperson1337/requestarr/internal/buildinfo"
)

// localAuthRoutes are the routes of the built-in local account (D040). Neither
// fits the four-role matrix: sign-in is public, and the password change works
// only for the one account that has a local password. local_auth_test.go
// covers both, for every role.
var localAuthRoutes = map[string]bool{"POST /auth/local/login": true, "POST /auth/local/password": true}

// The route table is the registry of every route, so coverage is read from it
// rather than inferred from source: a route can only exist by being registered
// there with an access rule, and a state-changing one needs a role-matrix row.
func TestEveryMutatingEndpointHasARoleMatrixRow(t *testing.T) {
	backend := &matrixBackend{t: t}
	repo := matrixAuthRepo{csrfAuthRepo{statusAuthRepo{user: auth.User{ID: matrixUserID, Role: "admin"}}}, backend}
	_, rt := newRouter(testLogger(), buildinfo.Info{}, backend, backend, backend, backend, &auth.Handler{Repo: repo, Secret: "test-secret", PublicURL: "http://example.test"})

	rows := map[string]mutationCase{}
	for _, row := range mutationCases() {
		rows[row.pattern] = row
	}
	registered := map[string]routePolicy{}
	for _, policy := range rt.Policies() {
		if !mutating(policy.Pattern) || localAuthRoutes[policy.Pattern] {
			continue
		}
		registered[policy.Pattern] = policy
		row, ok := rows[policy.Pattern]
		if !ok {
			t.Errorf("mutating endpoint %q has no four-role matrix row", policy.Pattern)
			continue
		}
		// A route the table makes administrator-only must be an admin row. The
		// reverse need not hold: a request transition is checked in the
		// requests service, not at the route.
		if policy.Access == accessPublic || policy.Access == accessAdmin && row.policy != "admin" || policy.Access == accessSuperuser && row.policy != "superuser" {
			t.Errorf("%s: matrix policy %q but route access is %v", policy.Pattern, row.policy, policy.Access)
		}
	}
	for pattern := range rows {
		if _, ok := registered[pattern]; !ok {
			t.Errorf("matrix row %q no longer matches a registered route", pattern)
		}
	}
}

// The request-on-behalf picker reads the person list as an admin, so that list
// must stay open to an admin and not move to the superuser-only routes.
func TestThePersonListThePickerUsesIsOpenToAnAdmin(t *testing.T) {
	backend := &matrixBackend{t: t}
	repo := matrixAuthRepo{csrfAuthRepo{statusAuthRepo{user: auth.User{ID: matrixUserID, Role: "admin"}}}, backend}
	_, rt := newRouter(testLogger(), buildinfo.Info{}, backend, backend, backend, backend, &auth.Handler{Repo: repo, Secret: "test-secret", PublicURL: "http://example.test"})
	for _, policy := range rt.Policies() {
		if policy.Pattern == "GET /admin/users" {
			if policy.Access != accessAdmin {
				t.Fatalf("GET /admin/users access = %v, want %v", policy.Access, accessAdmin)
			}
			return
		}
	}
	t.Fatal("GET /admin/users is not registered")
}
