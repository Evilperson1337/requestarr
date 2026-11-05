package authsettings_test

import (
	"github.com/evilperson1337/requestarr/internal/authsettings"
	"reflect"
	"testing"
)

func TestEnvironmentIsAnIndivisibleUnit(t *testing.T) {
	names := []string{"REQUESTARR_OIDC_ISSUER", "REQUESTARR_OIDC_CLIENT_ID", "REQUESTARR_OIDC_CLIENT_SECRET"}
	for mask := 0; mask < 8; mask++ {
		for _, saved := range []bool{false, true} {
			env := authsettings.Values{}
			fields := []*string{&env.Issuer, &env.ClientID, &env.ClientSecret}
			missing := []string{}
			for i, f := range fields {
				if mask&(1<<i) != 0 {
					*f = "environment"
				} else {
					missing = append(missing, names[i])
				}
			}
			var stored *authsettings.Values
			if saved {
				stored = &authsettings.Values{Issuer: "saved", ClientID: "saved", ClientSecret: "saved"}
			}
			got := authsettings.Resolve(env, stored)
			want := "environment_incomplete"
			switch {
			case mask == 0 && saved:
				want = "saved_only"
			case mask == 0:
				want = "not_configured"
			case mask == 7 && saved:
				want = "environment_overriding_saved"
			case mask == 7:
				want = "environment_only"
			}
			if got.State != want {
				t.Fatalf("mask %d saved %t: state %s", mask, saved, got.State)
			}
			if mask != 0 {
				if got.Effective != env {
					t.Fatal("mixed environment and saved configuration")
				}
				if !reflect.DeepEqual(got.Missing, missing) {
					t.Fatalf("mask %d: missing %v", mask, got.Missing)
				}
			}
			if saved && mask != 0 && len(got.Ignored) != 3 {
				t.Fatal("must report all ignored fields")
			}
		}
	}
	same := authsettings.Values{Issuer: "same", ClientID: "same", ClientSecret: "same"}
	if got := authsettings.Resolve(same, &same); got.State != "environment_overriding_saved" || len(got.Differing) != 0 || len(got.Ignored) != 3 {
		t.Fatal("equal saved values must still be reported as ignored")
	}
}
