package httpapi

import (
	"testing"

	"github.com/evilperson1337/requestarr/internal/auth"
	"github.com/evilperson1337/requestarr/internal/buildinfo"
)

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
		if !mutating(policy.Pattern) {
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
		if policy.Access == accessPublic || policy.Access == accessAdmin && row.policy != "admin" {
			t.Errorf("%s: matrix policy %q but route access is %v", policy.Pattern, row.policy, policy.Access)
		}
	}
	for pattern := range rows {
		if _, ok := registered[pattern]; !ok {
			t.Errorf("matrix row %q no longer matches a registered route", pattern)
		}
	}
}
