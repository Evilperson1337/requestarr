package domain

import "strings"

// GroupMapping is how the groups in a person's ID token decide their role.
// It is switched off until an operator turns it on, so an upgrade never
// promotes people because of group names nobody knew the provider sent.
type GroupMapping struct {
	Enabled bool
	// Claim is the ID-token claim that holds the groups.
	Claim string
	// Superuser, Admin, Requester and Viewer are the group names for each role.
	Superuser, Admin, Requester, Viewer string
}

// DefaultGroupMapping is what a setup starts with: off, with the default names.
func DefaultGroupMapping() GroupMapping {
	return GroupMapping{Claim: "groups", Superuser: "owner", Admin: "admin", Requester: "user", Viewer: "viewer"}
}

// WithDefaults fills every blank name with its default, so a configuration
// saved before the setting existed behaves like a fresh one.
func (m GroupMapping) WithDefaults() GroupMapping {
	d := DefaultGroupMapping()
	if m.Claim == "" {
		m.Claim = d.Claim
	}
	if m.Superuser == "" {
		m.Superuser = d.Superuser
	}
	if m.Admin == "" {
		m.Admin = d.Admin
	}
	if m.Requester == "" {
		m.Requester = d.Requester
	}
	if m.Viewer == "" {
		m.Viewer = d.Viewer
	}
	return m
}

// normalizeGroup makes names comparable: letter case and one leading slash
// (a path-style group such as /owner) are ignored.
func normalizeGroup(g string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(g), "/"))
}

// RoleFor returns the most powerful role any of the groups maps to. ok is
// false when mapping is off or no group matches, in which case the person
// keeps their role.
func (m GroupMapping) RoleFor(groups []string) (Role, bool) {
	if !m.Enabled {
		return "", false
	}
	names := []struct {
		name string
		role Role
	}{{m.Superuser, RoleSuperuser}, {m.Admin, RoleAdmin}, {m.Requester, RoleRequester}, {m.Viewer, RoleViewer}}
	best, found := Role(""), false
	for _, g := range groups {
		g = normalizeGroup(g)
		if g == "" {
			continue
		}
		for _, n := range names {
			if normalizeGroup(n.name) == g && n.role.rank() > best.rank() {
				best, found = n.role, true
			}
		}
	}
	return best, found
}
