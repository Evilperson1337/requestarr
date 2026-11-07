package domain

import (
	"errors"
	"strings"
	"testing"
)

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
		{RoleSuperuser, Capabilities{Request: true, ManageRequests: true, ManageSystem: true, ManageAccess: true}},
		{"owner", Capabilities{}},
		{"", Capabilities{}},
	}
	for _, c := range cases {
		if got := c.role.Capabilities(); got != c.want {
			t.Errorf("%q.Capabilities() = %+v, want %+v", c.role, got, c.want)
		}
	}
}

func TestAllFourRolesAreStored(t *testing.T) {
	for role, want := range map[Role]bool{RoleViewer: true, RoleRequester: true, RoleAdmin: true, RoleSuperuser: true, "owner": false, "": false} {
		if got := role.Stored(); got != want {
			t.Errorf("%q.Stored() = %v, want %v", role, got, want)
		}
	}
}

func TestWhoMayChangeWhichRole(t *testing.T) {
	cases := []struct {
		actor, from, to Role
		needs           Role // empty means allowed
	}{
		{RoleSuperuser, RoleViewer, RoleSuperuser, ""},
		{RoleSuperuser, RoleSuperuser, RoleViewer, ""},
		{RoleSuperuser, RoleAdmin, RoleRequester, ""},
		{RoleSuperuser, RoleRequester, RoleAdmin, ""},
		{RoleAdmin, RoleViewer, RoleRequester, ""},
		{RoleAdmin, RoleRequester, RoleViewer, ""},
		{RoleAdmin, RoleRequester, RoleAdmin, RoleSuperuser},
		{RoleAdmin, RoleRequester, RoleSuperuser, RoleSuperuser},
		{RoleAdmin, RoleAdmin, RoleViewer, RoleSuperuser},
		{RoleAdmin, RoleSuperuser, RoleViewer, RoleSuperuser},
		{RoleRequester, RoleViewer, RoleRequester, RoleAdmin},
		{RoleViewer, RoleViewer, RoleRequester, RoleAdmin},
		{RoleSuperuser, RoleViewer, "owner", RoleSuperuser},
	}
	for _, c := range cases {
		err := c.actor.CheckRoleChange(c.from, c.to)
		var refusal *RoleChangeRefused
		switch {
		case c.needs == "" && err != nil:
			t.Errorf("%s changing %s to %s refused: %v", c.actor, c.from, c.to, err)
		case c.needs != "" && !errors.As(err, &refusal):
			t.Errorf("%s changing %s to %s allowed, want refusal needing %s", c.actor, c.from, c.to, c.needs)
		case c.needs != "" && refusal.Needs != c.needs:
			t.Errorf("%s changing %s to %s needs %s, want %s", c.actor, c.from, c.to, refusal.Needs, c.needs)
		case c.needs != "" && !strings.Contains(err.Error(), string(c.needs)):
			t.Errorf("message %q does not name the role %s", err, c.needs)
		}
	}
}

func TestOnlyASuperuserKeepsTheInstanceSignable(t *testing.T) {
	if got := RolesThatManageAccess(); len(got) != 1 || got[0] != "superuser" {
		t.Errorf("RolesThatManageAccess() = %v, want [superuser]", got)
	}
}

func TestNobodyIsOfferedAChangeToTheBuiltInAccountOrThemselves(t *testing.T) {
	if got := RoleSuperuser.RoleChoicesFor(RoleViewer, false, false); len(got) != 4 {
		t.Errorf("a superuser changing another person: %v, want all four roles", got)
	}
	if got := RoleSuperuser.RoleChoicesFor(RoleSuperuser, true, false); got == nil || len(got) != 0 {
		t.Errorf("the built-in account: %v, want an empty, non-nil list", got)
	}
	if got := RoleSuperuser.RoleChoicesFor(RoleViewer, false, true); got == nil || len(got) != 0 {
		t.Errorf("oneself: %v, want an empty, non-nil list", got)
	}
}

func TestRoleListsForSQL(t *testing.T) {
	if got := RolesThatManageRequests(); len(got) != 2 || got[0] != "admin" || got[1] != "superuser" {
		t.Errorf("RolesThatManageRequests() = %v, want [admin superuser]", got)
	}
}
