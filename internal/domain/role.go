package domain

import "strings"

// Role is what a person may do in Requestarr. A first sign-in default is
// never Admin (D035).
//
// This file is the one place that knows what a role may do. Every other
// module asks a Role (or the Capabilities derived from it) and never compares
// a role name; role_literals_test.go enforces that.
type Role string

const (
	RoleViewer    Role = "viewer"
	RoleRequester Role = "requester"
	RoleAdmin     Role = "admin"
	// RoleSuperuser ranks above Admin: the owner, with nothing withheld.
	RoleSuperuser Role = "superuser"
)

// Capabilities is what a signed-in person may do. The server returns it with
// the session so the web app never compares role names.
type Capabilities struct {
	Request        bool `json:"request"`
	ManageRequests bool `json:"manageRequests"`
	ManageSystem   bool `json:"manageSystem"`
	ManageAccess   bool `json:"manageAccess"`
}

// rank orders the roles by power; an unknown role has rank 0 and may do nothing.
func (r Role) rank() int {
	switch r {
	case RoleViewer:
		return 1
	case RoleRequester:
		return 2
	case RoleAdmin:
		return 3
	case RoleSuperuser:
		return 4
	}
	return 0
}

// Known reports whether r is one of the four roles.
func (r Role) Known() bool { return r.rank() > 0 }

// Stored reports whether r can be written to the users table.
func (r Role) Stored() bool { return r.Known() }

// CanManageAccess reports whether the role may do what only the owner may:
// give or take the admin and superuser roles. Later tickets extend it to
// sign-in, credentials and the local administrator.
func (r Role) CanManageAccess() bool { return r.rank() >= RoleSuperuser.rank() }

// RoleChangeRefused is the refusal to change someone's role. Needs names the
// role that could have done it, so the message can say what to ask for.
type RoleChangeRefused struct {
	Needs  Role
	Reason string
}

func (e *RoleChangeRefused) Error() string {
	return e.Reason + " (the " + string(e.Needs) + " role is required)"
}

// CheckRoleChange reports whether a person holding r may move someone from one
// role to another. A superuser may set anyone to any role. An admin may move a
// person only between requester and viewer, never touching an admin or a
// superuser. Anyone else may change nobody's role. Changing your own role and
// the built-in account are guarded by the store, which knows who is who.
func (r Role) CheckRoleChange(from, to Role) error {
	if !r.CanManageSystem() {
		return &RoleChangeRefused{Needs: RoleAdmin, Reason: "changing a person's role is for administrators"}
	}
	if !to.Known() {
		return &RoleChangeRefused{Needs: RoleSuperuser, Reason: "that is not a role"}
	}
	if r.CanManageAccess() {
		return nil
	}
	if from.CanManageSystem() {
		return &RoleChangeRefused{Needs: RoleSuperuser, Reason: "only a superuser can change an admin or a superuser"}
	}
	if to.CanManageSystem() {
		return &RoleChangeRefused{Needs: RoleSuperuser, Reason: "only a superuser can make someone an admin or a superuser"}
	}
	return nil
}

// CanRequest reports whether the role may create and extend requests.
func (r Role) CanRequest() bool { return r.rank() >= RoleRequester.rank() }

// CanManageRequests reports whether the role may approve, deny and cancel
// requests, handle issues and see the awaiting count.
func (r Role) CanManageRequests() bool { return r.rank() >= RoleAdmin.rank() }

// CanManageSystem reports whether the role may use the administrative
// surface (Settings, people, matches). Until superuser-only routes exist it
// equals CanManageRequests.
func (r Role) CanManageSystem() bool { return r.rank() >= RoleAdmin.rank() }

// Capabilities derives the session answer from the role.
func (r Role) Capabilities() Capabilities {
	return Capabilities{Request: r.CanRequest(), ManageRequests: r.CanManageRequests(), ManageSystem: r.CanManageSystem(), ManageAccess: r.CanManageAccess()}
}

// ValidDefault reports whether the role can be the default for people who sign
// in for the first time: viewer or requester, never admin.
func (r Role) ValidDefault() bool { return r == RoleViewer || r == RoleRequester }

// StoredRoles lists the roles that can be stored, weakest first.
func StoredRoles() []Role { return []Role{RoleViewer, RoleRequester, RoleAdmin, RoleSuperuser} }

// RoleChoices lists the roles a person holding r may give someone who now has
// the role from, weakest first. Empty means r may not change that person.
func (r Role) RoleChoices(from Role) []Role {
	choices := []Role{}
	for _, to := range StoredRoles() {
		if r.CheckRoleChange(from, to) == nil {
			choices = append(choices, to)
		}
	}
	return choices
}

// RoleChoicesFor is RoleChoices for a listed person: nobody is offered a change
// to their own role, and the built-in account's role is fixed. The list is
// never nil, so it encodes as [].
func (r Role) RoleChoicesFor(from Role, builtin, self bool) []Role {
	if builtin || self {
		return []Role{}
	}
	return r.RoleChoices(from)
}

// RolesThatManageRequests lists the stored roles for which CanManageRequests
// holds, as strings for SQLRoleList.
func RolesThatManageRequests() []string { return rolesWhere(Role.CanManageRequests) }

// RolesThatManageAccess lists the stored roles for which CanManageAccess holds.
func RolesThatManageAccess() []string { return rolesWhere(Role.CanManageAccess) }

// RolesThatManageSystem lists the stored roles for which CanManageSystem holds.
func RolesThatManageSystem() []string { return rolesWhere(Role.CanManageSystem) }

func rolesWhere(ok func(Role) bool) []string {
	var out []string
	for _, r := range StoredRoles() {
		if ok(r) {
			out = append(out, string(r))
		}
	}
	return out
}

// SQLRoleList renders roles as a quoted, comma-separated list for
// `role IN (...)`. The values are the module's own constants, never input.
func SQLRoleList(roles []string) string {
	quoted := make([]string, len(roles))
	for i, r := range roles {
		quoted[i] = "'" + r + "'"
	}
	return strings.Join(quoted, ",")
}

// DiagnosticsTokenRole is the role a diagnostics token acts with (D043). It is
// what the role gates of the routes marked token-readable need; the route
// table, not the role, keeps a token away from every other route.
const DiagnosticsTokenRole = RoleSuperuser
