package authsettings_test

import (
	"context"
	"errors"
	"testing"

	"github.com/evilperson1337/requestarr/internal/auth/oidc"
	"github.com/evilperson1337/requestarr/internal/authsettings"
	"github.com/evilperson1337/requestarr/internal/domain"
)

func groupUpdate(mut func(*authsettings.Settings)) authsettings.Update {
	u := scopeUpdate("openid profile email groups")
	if mut != nil {
		mut(&u.Settings)
	}
	return u
}

func okFactory(context.Context, authsettings.Values, string) (oidc.Provider, error) {
	return &provider{}, nil
}

func TestANewSetupShowsTheDefaultGroupsAndMappingIsOff(t *testing.T) {
	svc := scopeService(t, &memory{}, nil)
	st, _ := svc.Status(context.Background())
	e := st.Effective
	if e.AssignRolesFromGroups || e.GroupsClaim != "groups" || e.OwnerGroup != "owner" || e.AdminGroup != "admin" || e.UserGroup != "user" || e.ViewerGroup != "viewer" {
		t.Fatalf("defaults = %+v", e)
	}
}

func TestSavedGroupSettingsApplyAtOnceThroughTheSnapshot(t *testing.T) {
	ctx := context.Background()
	repo := &memory{}
	svc := scopeService(t, repo, okFactory)
	if err := svc.Save(ctx, "a", groupUpdate(nil)); err != nil {
		t.Fatal(err)
	}
	if svc.Snapshot().Groups.Enabled {
		t.Fatal("mapping must stay off until switched on")
	}
	on := groupUpdate(func(s *authsettings.Settings) {
		s.AssignRolesFromGroups, s.OwnerGroup, s.GroupsClaim = true, "Household", "roles"
	})
	if err := svc.Save(ctx, "a", on); err != nil {
		t.Fatal(err)
	}
	m := svc.Snapshot().Groups
	if !m.Enabled || m.Claim != "roles" || m.Superuser != "Household" || m.Admin != "admin" || m.Requester != "user" || m.Viewer != "viewer" {
		t.Fatalf("snapshot mapping = %+v", m)
	}
	if role, ok := m.RoleFor([]string{"household"}); !ok || role != domain.RoleSuperuser {
		t.Fatal("the saved names must decide roles straight away")
	}
	st, _ := svc.Status(ctx)
	if !st.Effective.AssignRolesFromGroups || st.Effective.GroupsClaim != "roles" || st.Effective.OwnerGroup != "Household" {
		t.Fatalf("status = %+v", st.Effective)
	}
}

func TestTheGroupsClaimReachesTheProviderFactory(t *testing.T) {
	var got string
	svc := scopeService(t, &memory{}, func(_ context.Context, v authsettings.Values, _ string) (oidc.Provider, error) {
		got = v.GroupsClaim
		return &provider{}, nil
	})
	if err := svc.Save(context.Background(), "a", groupUpdate(func(s *authsettings.Settings) { s.GroupsClaim = "memberOf" })); err != nil {
		t.Fatal(err)
	}
	if got != "memberOf" {
		t.Fatalf("factory got claim %q", got)
	}
}

func TestBlankGroupFieldsSaveAsTheirDefaults(t *testing.T) {
	repo := &memory{}
	svc := scopeService(t, repo, okFactory)
	if err := svc.Save(context.Background(), "a", groupUpdate(func(s *authsettings.Settings) { s.AssignRolesFromGroups = true })); err != nil {
		t.Fatal(err)
	}
	s := repo.record.Settings
	if s.GroupsClaim != "groups" || s.OwnerGroup != "owner" || s.AdminGroup != "admin" || s.UserGroup != "user" || s.ViewerGroup != "viewer" {
		t.Fatalf("stored %+v", s)
	}
}

func TestUnsafeGroupFieldsAreRefused(t *testing.T) {
	for _, mut := range []func(*authsettings.Settings){
		func(s *authsettings.Settings) { s.GroupsClaim = "a\x00b" },
		func(s *authsettings.Settings) { s.AdminGroup = " padded " },
		func(s *authsettings.Settings) { s.ViewerGroup = string(make([]byte, 300)) },
	} {
		repo := &memory{}
		svc := scopeService(t, repo, okFactory)
		if err := svc.Save(context.Background(), "a", groupUpdate(mut)); !errors.Is(err, authsettings.ErrInvalid) || repo.record != nil {
			t.Errorf("accepted: %v", err)
		}
	}
}

func TestAnExistingConfigurationKeepsMappingOffAfterAnUpgrade(t *testing.T) {
	repo := &memory{record: &authsettings.Record{Settings: authsettings.Settings{Enabled: false, ProviderName: "Keycloak", Issuer: "https://id.test", ClientID: "id", Scopes: authsettings.LegacyScopes}}}
	svc := scopeService(t, repo, okFactory)
	st, _ := svc.Status(context.Background())
	if st.Effective.AssignRolesFromGroups || st.Effective.GroupsClaim != "groups" || st.Effective.AdminGroup != "admin" {
		t.Fatalf("legacy record shows %+v", st.Effective)
	}
}

func TestEnablingASavedConfigurationKeepsItsGroupSettings(t *testing.T) {
	ctx := context.Background()
	repo := &memory{}
	svc := scopeService(t, repo, okFactory)
	u := groupUpdate(func(s *authsettings.Settings) { s.Enabled, s.AssignRolesFromGroups, s.AdminGroup = false, true, "ops" })
	if err := svc.Save(ctx, "a", u); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetEnabled(ctx, "a", true); err != nil {
		t.Fatal(err)
	}
	if m := svc.Snapshot().Groups; !m.Enabled || m.Admin != "ops" {
		t.Fatalf("mapping after enabling = %+v", m)
	}
}

func TestEnvironmentConfigurationDoesNotMapGroups(t *testing.T) {
	svc, err := authsettings.New(context.Background(), &memory{}, nil, authsettings.Values{Issuer: "https://id.test", ClientID: "id", ClientSecret: "s"}, "https://app.test", okFactory)
	if err != nil {
		t.Fatal(err)
	}
	if svc.Snapshot().Groups.Enabled {
		t.Fatal("the environment has no group setting, so mapping stays off")
	}
	st, _ := svc.Status(context.Background())
	if !st.ReadOnly || st.Effective.AssignRolesFromGroups || st.Effective.OwnerGroup != "owner" {
		t.Fatalf("status = %+v", st.Effective)
	}
}
