//go:build integration

package postgres

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"
)

// A server that answers but refuses the login or the database is a different
// problem from an unreachable one, and the message must say which.
func TestNewPoolNamesARejectedLoginAndAMissingDatabaseIntegration(t *testing.T) {
	base, err := url.Parse(testDatabaseURL(t))
	if err != nil {
		t.Fatalf("setup failed (%T)", err)
	}
	cases := []struct {
		name   string
		modify func(u *url.URL)
		want   string
	}{
		{"wrong password", func(u *url.URL) { u.User = url.UserPassword(u.User.Username(), "CANARY-WRONG-PASSWORD") }, "rejected the user name or password"},
		{"database does not exist", func(u *url.URL) { u.Path = "/no_such_database_for_the_test" }, `"no_such_database_for_the_test" does not exist`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			u := *base
			c.modify(&u)
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			_, err := newPool(ctx, u.String())
			if err == nil {
				t.Fatal("expected a connection failure")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("message %q does not name the cause (missing %q)", err.Error(), c.want)
			}
			if strings.Contains(err.Error(), "CANARY-WRONG-PASSWORD") {
				t.Fatalf("message leaks the password: %q", err.Error())
			}
		})
	}
}
