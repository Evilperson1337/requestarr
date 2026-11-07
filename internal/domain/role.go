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
	// RoleSuperuser ranks above Admin. It is not stored yet: the three stored
	// roles map onto the module unchanged until the migration that widens them.
	RoleSuperuser Role = "superuser"
)

// Capabilities is what a signed-in person may do. The server returns it with
// the session so the web app never compares role names.
type Capabilities struct {
	Request        bool `json:"request"`
	ManageRequests bool `json:"manageRequests"`
	ManageSystem   bool `json:"manageSystem"`
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

// Stored reports whether r can be written to the users table today.
func (r Role) Stored() bool { return r != RoleSuperuser && r.Known() }

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
	return Capabilities{Request: r.CanRequest(), ManageRequests: r.CanManageRequests(), ManageSystem: r.CanManageSystem()}
}

// ValidDefault reports whether the role can be the default for people who sign
// in for the first time: viewer or requester, never admin.
func (r Role) ValidDefault() bool { return r == RoleViewer || r == RoleRequester }

// StoredRoles lists the roles that can be stored, weakest first.
func StoredRoles() []Role { return []Role{RoleViewer, RoleRequester, RoleAdmin} }

// RolesThatManageRequests lists the stored roles for which CanManageRequests
// holds, as strings for SQLRoleList.
func RolesThatManageRequests() []string { return rolesWhere(Role.CanManageRequests) }

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
