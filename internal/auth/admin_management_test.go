package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakeManagementRepo evaluates the guard the way the store does: after its
// own checks, as part of the one serialised section.
type fakeManagementRepo struct {
	status     LocalAdminStatus
	otherAdmin bool
	guardRuns  int
	disabled   bool
}

func (f *fakeManagementRepo) LocalAdminStatus(context.Context) (LocalAdminStatus, error) {
	return f.status, nil
}
func (f *fakeManagementRepo) CreateRecoveryAdmin(context.Context, string, string, time.Time) (bool, error) {
	return true, nil
}
func (f *fakeManagementRepo) SetLocalAdminDisabled(_ context.Context, _ string, disabled bool, _ time.Time, guard func() bool) error {
	if disabled {
		if !f.otherAdmin {
			return ErrLastLocalAdmin
		}
		f.guardRuns++
		if guard != nil && !guard() {
			return ErrNoWorkingSignIn
		}
	}
	f.disabled = disabled
	return nil
}

func TestDisablingNeedsAnotherAdminWhoCanReallySignIn(t *testing.T) {
	for _, tc := range []struct {
		name       string
		otherAdmin bool
		oidc       func() bool
		want       error
	}{
		{"no other admin", false, func() bool { return true }, ErrLastLocalAdmin},
		{"other admin, OIDC off or broken", true, func() bool { return false }, ErrNoWorkingSignIn},
		{"other admin, OIDC state unknown", true, nil, ErrNoWorkingSignIn},
		{"other admin and OIDC working", true, func() bool { return true }, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeManagementRepo{otherAdmin: tc.otherAdmin}
			svc := AdminManagement{Repo: repo, OIDCWorking: tc.oidc}
			err := svc.SetDisabled(context.Background(), "actor", true)
			if !errors.Is(err, tc.want) && err != tc.want {
				t.Fatalf("err=%v want %v", err, tc.want)
			}
			if repo.disabled != (tc.want == nil) {
				t.Fatalf("disabled=%v", repo.disabled)
			}
		})
	}
}

func TestEnablingNeverNeedsOIDC(t *testing.T) {
	repo := &fakeManagementRepo{disabled: true}
	if err := (AdminManagement{Repo: repo}).SetDisabled(context.Background(), "actor", false); err != nil || repo.disabled {
		t.Fatalf("err=%v disabled=%v", err, repo.disabled)
	}
}

func TestStatusExplainsWhyTheAccountCannotBeDisabled(t *testing.T) {
	for _, tc := range []struct {
		name                         string
		otherAdmin, oidc             bool
		wantCan, wantOther, wantOIDC bool
	}{
		{"nobody else", false, true, false, false, true},
		{"other admin, OIDC off", true, false, false, true, false},
		{"other admin, OIDC working", true, true, true, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeManagementRepo{status: LocalAdminStatus{Exists: true, OtherAdmin: tc.otherAdmin, CanDisable: tc.otherAdmin}}
			oidc := tc.oidc
			st, err := (AdminManagement{Repo: repo, OIDCWorking: func() bool { return oidc }}).LocalAdminStatus(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if st.CanDisable != tc.wantCan || st.OtherAdmin != tc.wantOther || st.OIDCWorking != tc.wantOIDC {
				t.Fatalf("status=%+v", st)
			}
		})
	}
}
