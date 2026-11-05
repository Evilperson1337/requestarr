package domain

import "testing"

func TestOnlyViewerAndRequesterAreValidDefaults(t *testing.T) {
	for role, want := range map[Role]bool{RoleViewer: true, RoleRequester: true, RoleAdmin: false, "owner": false, "": false} {
		if got := role.ValidDefault(); got != want {
			t.Errorf("%q.ValidDefault() = %v, want %v", role, got, want)
		}
	}
}
