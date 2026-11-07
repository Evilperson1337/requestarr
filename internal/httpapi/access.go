package httpapi

import (
	"net/http"
	"sort"
	"strings"

	"github.com/evilperson1337/requestarr/internal/apierror"
	"github.com/evilperson1337/requestarr/internal/auth"
	"github.com/evilperson1337/requestarr/internal/domain"
)

// access is who may call a route.
type access int

const (
	// accessPublic needs no session.
	accessPublic access = iota
	// accessSession needs any signed-in user.
	accessSession
	// accessAdmin needs a signed-in administrator.
	accessAdmin
)

func (a access) String() string {
	switch a {
	case accessPublic:
		return "public"
	case accessSession:
		return "session"
	}
	return "manage"
}

// routePolicy is the access rule of one route. CSRF is required for every
// route that changes state, except the auth routes that check it themselves.
type routePolicy struct {
	Pattern string // "METHOD /path", as registered on the API mux
	Access  access
	CSRF    bool
}

// routeTable is the one place routes are registered and their access and CSRF
// policy recorded, so the policy cannot drift from the routes. The request
// pipeline and the role tests both read it.
type routeTable struct {
	mux      *http.ServeMux
	auth     *auth.Handler
	policies map[string]routePolicy
}

func newRouteTable(mux *http.ServeMux, authHandler *auth.Handler) *routeTable {
	return &routeTable{mux: mux, auth: authHandler, policies: map[string]routePolicy{}}
}

func mutating(pattern string) bool {
	method, _, _ := strings.Cut(pattern, " ")
	switch method {
	case "GET", "HEAD", "OPTIONS":
		return false
	}
	return true
}

// handle registers a route with its access rule. The wrapper answers 401 and
// 403 before the handler runs, so handlers never repeat the gate. A state-changing
// route is CSRF-protected.
func (t *routeTable) handle(pattern string, a access, fn func(http.ResponseWriter, *http.Request, auth.User)) {
	t.policies[pattern] = routePolicy{Pattern: pattern, Access: a, CSRF: a != accessPublic && mutating(pattern)}
	t.mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		var user auth.User
		if a != accessPublic {
			if t.auth == nil {
				apierror.Write(w, r, http.StatusUnauthorized, "authentication_required", "authentication required")
				return
			}
			var err error
			if user, err = t.auth.CurrentSession(r); err != nil {
				apierror.Write(w, r, http.StatusUnauthorized, "authentication_required", "authentication required")
				return
			}
			if user.MustChange {
				apierror.Write(w, r, http.StatusForbidden, "password_change_required", "choose a new password before continuing")
				return
			}
			if a == accessAdmin && !domain.Role(user.Role).CanManageSystem() {
				apierror.Write(w, r, http.StatusForbidden, "forbidden", "administrator role required")
				return
			}
		}
		fn(w, r, user)
	})
}

// record notes a route registered elsewhere (the auth handler's own routes).
func (t *routeTable) record(pattern string, a access, csrf bool) {
	t.policies[pattern] = routePolicy{Pattern: pattern, Access: a, CSRF: csrf}
}

// policyFor is the policy of the route that serves r, or false when no route
// matches. path is the request path without the /api/v1 prefix.
func (t *routeTable) policyFor(r *http.Request, path string) (routePolicy, bool) {
	probe := r.Clone(r.Context())
	probe.URL.Path = path
	probe.URL.RawPath = ""
	_, pattern := t.mux.Handler(probe)
	policy, ok := t.policies[pattern]
	return policy, ok
}

// Policies lists every registered route, sorted by pattern.
func (t *routeTable) Policies() []routePolicy {
	out := make([]routePolicy, 0, len(t.policies))
	for _, p := range t.policies {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Pattern < out[j].Pattern })
	return out
}
