package authsettings

import "github.com/evilperson1337/requestarr/internal/domain"

// groupSettings are the "Assign roles from groups" fields of Settings. They
// are not secrets: names of groups the operator chose, never any person's.
// A blank field means its default, so a configuration saved before the setting
// existed behaves like a new one, with mapping off.

// mappingOf is the mapping a Settings value describes, defaults filled in.
func mappingOf(s Settings) domain.GroupMapping {
	return domain.GroupMapping{
		Enabled: s.AssignRolesFromGroups, Claim: s.GroupsClaim,
		Superuser: s.OwnerGroup, Admin: s.AdminGroup, Requester: s.UserGroup, Viewer: s.ViewerGroup,
	}.WithDefaults()
}

// withGroupDefaults returns s with every blank group field replaced by its
// default, and ok false when a field is not plain text of a sensible length.
func withGroupDefaults(s Settings) (Settings, bool) {
	m := mappingOf(s)
	for _, f := range []string{m.Claim, m.Superuser, m.Admin, m.Requester, m.Viewer} {
		if !validText(f, 200) {
			return s, false
		}
	}
	s.GroupsClaim, s.OwnerGroup, s.AdminGroup, s.UserGroup, s.ViewerGroup = m.Claim, m.Superuser, m.Admin, m.Requester, m.Viewer
	return s, true
}

// effectiveMapping is what sign-in uses. The environment configures no group
// settings, so with it mapping is off; so it is with no saved configuration.
func effectiveMapping(fromEnv bool, record *Record) domain.GroupMapping {
	if fromEnv || record == nil {
		return domain.DefaultGroupMapping()
	}
	return mappingOf(record.Settings)
}

// showGroups copies a mapping into the fields the page reads.
func showGroups(s Settings, m domain.GroupMapping) Settings {
	s.AssignRolesFromGroups, s.GroupsClaim = m.Enabled, m.Claim
	s.OwnerGroup, s.AdminGroup, s.UserGroup, s.ViewerGroup = m.Superuser, m.Admin, m.Requester, m.Viewer
	return s
}
