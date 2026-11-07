package domain_test

import (
	"testing"

	"github.com/evilperson1337/requestarr/internal/domain"
)

func TestGroupMappingPicksTheHighestMatchingRole(t *testing.T) {
	m := domain.DefaultGroupMapping()
	m.Enabled = true
	cases := []struct {
		name   string
		groups []string
		want   domain.Role
		ok     bool
	}{
		{"owner", []string{"owner"}, domain.RoleSuperuser, true},
		{"admin", []string{"admin"}, domain.RoleAdmin, true},
		{"user group is requester", []string{"user"}, domain.RoleRequester, true},
		{"viewer", []string{"viewer"}, domain.RoleViewer, true},
		{"most powerful wins", []string{"viewer", "admin", "user"}, domain.RoleAdmin, true},
		{"a second group never reduces access", []string{"user", "owner", "viewer"}, domain.RoleSuperuser, true},
		{"letter case ignored", []string{"ADMIN"}, domain.RoleAdmin, true},
		{"one leading slash ignored", []string{"/owner"}, domain.RoleSuperuser, true},
		{"two slashes are not one", []string{"//owner"}, "", false},
		{"nested path is not a match", []string{"/team/owner"}, "", false},
		{"unmapped groups", []string{"staff", "everyone"}, "", false},
		{"no groups", nil, "", false},
	}
	for _, c := range cases {
		got, ok := m.RoleFor(c.groups)
		if got != c.want || ok != c.ok {
			t.Errorf("%s: got (%q,%v), want (%q,%v)", c.name, got, ok, c.want, c.ok)
		}
	}
}

func TestGroupMappingUsesCustomNamesAndIgnoresBlankOnes(t *testing.T) {
	m := domain.GroupMapping{Enabled: true, Claim: "roles", Superuser: "Household-Owners", Admin: "", Requester: "kids", Viewer: "guests"}
	if r, ok := m.RoleFor([]string{"household-owners"}); !ok || r != domain.RoleSuperuser {
		t.Fatalf("custom name: %q %v", r, ok)
	}
	if _, ok := m.RoleFor([]string{"", "/"}); ok {
		t.Fatal("a blank mapped name must match nothing")
	}
	if _, ok := m.RoleFor([]string{"admin"}); ok {
		t.Fatal("default names do not apply once changed")
	}
}

func TestGroupMappingOffAppliesNothing(t *testing.T) {
	m := domain.DefaultGroupMapping()
	if _, ok := m.RoleFor([]string{"owner"}); ok {
		t.Fatal("mapping is off by default")
	}
	if m.Claim != "groups" || m.Superuser != "owner" || m.Admin != "admin" || m.Requester != "user" || m.Viewer != "viewer" {
		t.Fatalf("defaults: %+v", m)
	}
}
