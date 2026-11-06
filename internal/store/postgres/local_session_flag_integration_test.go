//go:build integration

package postgres

import (
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/auth"
)

// K1: the store marks the built-in local administrator's session, and only that.
func TestSessionsKnowWhetherTheyAreTheLocalAdministratorIntegration(t *testing.T) {
	ctx, s, other, local := newManagedInstance(t)
	for _, tc := range []struct {
		user string
		want bool
	}{{local, true}, {other, false}} {
		token := auth.Hash("secret", "token-"+tc.user)
		if err := s.CreateSession(ctx, tc.user, token, auth.Hash("secret", "csrf"), time.Now().Add(time.Hour)); err != nil {
			t.Fatal("session setup failed")
		}
		u, _, _, err := s.GetSession(ctx, token, time.Now())
		if err != nil || u.LocalAdministrator != tc.want {
			t.Fatalf("user %s: err=%v LocalAdministrator=%v, want %v", tc.user, err, u.LocalAdministrator, tc.want)
		}
	}
}
