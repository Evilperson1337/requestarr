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
	// accessSuperuser needs a superuser: sign-in, credentials and who is an admin.
	accessSuperuser
)

// The refusals a role gate gives; each names the role that is enough.
const (
	messageSuperuserRequired = "the superuser role is required"
	messageAdminRequired     = "the admin role is required"
	messageRequesterRequired = "the requester role or higher is required"
)

func (a access) String() string {
	switch a {
	case accessPublic:
		return "public"
	case accessSession:
		return "session"
	case accessSuperuser:
		return string(domain.RoleSuperuser)
	}
	return string(domain.RoleAdmin)
}

// routePolicy is the access rule of one route. CSRF is required for every
// route that changes state, except the auth routes that check it themselves.
type routePolicy struct {
	Pattern string // "METHOD /path", as registered on the API mux
	Access  access
	CSRF    bool
	// TokenReadable marks a GET route a diagnostics token (D043) may call. It
	// is set only by markTokenReadable, so no route is readable by accident.
	TokenReadable bool
}

// tokenUserKey carries the principal of a diagnostics token from the request
// pipeline to the route wrapper, which then skips the session lookup.
type tokenUserKey struct{}

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
		if tokenUser, ok := r.Context().Value(tokenUserKey{}).(auth.User); ok {
			// The pipeline already judged the bearer token for this route, which
			// is marked token-readable. The token acts as a superuser, so the
			// role gates below pass for the five routes it may reach.
			user = tokenUser
		} else if a != accessPublic {
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
			role := domain.Role(user.Role)
			if a == accessSuperuser && !role.CanManageAccess() {
				apierror.Write(w, r, http.StatusForbidden, "forbidden", messageSuperuserRequired)
				return
			}
			if a == accessAdmin && !role.CanManageSystem() {
				apierror.Write(w, r, http.StatusForbidden, "forbidden", messageAdminRequired)
				return
			}
		}
		fn(w, r, user)
	})
}

// markTokenReadable lets a diagnostics token call an already registered route.
// Only a GET route may carry it; anything else is a programming error found at
// startup.
func (t *routeTable) markTokenReadable(pattern string) {
	p, ok := t.policies[pattern]
	if !ok || mutating(pattern) || !strings.HasPrefix(pattern, "GET ") {
		panic("httpapi: only a registered GET route can be token-readable: " + pattern)
	}
	p.TokenReadable = true
	t.policies[pattern] = p
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
