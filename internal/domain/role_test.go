package domain

import "testing"

func TestOnlyViewerAndRequesterAreValidDefaults(t *testing.T) {
	for role, want := range map[Role]bool{RoleViewer: true, RoleRequester: true, RoleAdmin: false, "owner": false, "": false} {
		if got := role.ValidDefault(); got != want {
			t.Errorf("%q.ValidDefault() = %v, want %v", role, got, want)
		}
	}
}

func TestWhatEachRoleMayDo(t *testing.T) {
	cases := []struct {
		role Role
		want Capabilities
	}{
		{RoleViewer, Capabilities{}},
		{RoleRequester, Capabilities{Request: true}},
		{RoleAdmin, Capabilities{Request: true, ManageRequests: true, ManageSystem: true}},
		{RoleSuperuser, Capabilities{Request: true, ManageRequests: true, ManageSystem: true}},
		{"owner", Capabilities{}},
		{"", Capabilities{}},
	}
	for _, c := range cases {
		if got := c.role.Capabilities(); got != c.want {
			t.Errorf("%q.Capabilities() = %+v, want %+v", c.role, got, c.want)
		}
	}
}

func TestSuperuserIsNotStoredYet(t *testing.T) {
	for role, want := range map[Role]bool{RoleViewer: true, RoleRequester: true, RoleAdmin: true, RoleSuperuser: false, "owner": false} {
		if got := role.Stored(); got != want {
			t.Errorf("%q.Stored() = %v, want %v", role, got, want)
		}
	}
}

func TestRoleListsForSQL(t *testing.T) {
	if got := RolesThatManageRequests(); len(got) != 1 || got[0] != "admin" {
		t.Errorf("RolesThatManageRequests() = %v, want [admin]", got)
	}
}
