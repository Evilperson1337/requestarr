package postgres

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

// The driver's own error text can echo the connection string, so the pool never
// returns it. That left every failure as the same sentence, and an operator on a
// container host (no shell to test from) could not tell a name that does not
// resolve from a refused connection or a wrong password. The message must name
// the cause with fixed words, and must never carry a credential.
func TestNewPoolNamesTheCauseOfAFailedConnection(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("setup failed (%T)", err)
	}
	closedPort := listener.Addr().(*net.TCPAddr).Port
	listener.Close()

	cases := []struct {
		name string
		url  string
		want []string
	}{
		{"the host name does not resolve", "postgres://app:CANARY-PASSWORD@no-such-database-host.invalid:5432/requestarr", []string{"no-such-database-host.invalid", "could not be resolved", "user-defined"}},
		{"nothing is listening", fmt.Sprintf("postgres://app:CANARY-PASSWORD@127.0.0.1:%d/requestarr", closedPort), []string{fmt.Sprintf("127.0.0.1:%d", closedPort), "accepted the connection"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			_, err := newPool(ctx, c.url)
			if err == nil {
				t.Fatal("expected a connection failure")
			}
			message := err.Error()
			if !strings.HasPrefix(message, "database is unreachable") {
				t.Fatalf("message must keep its existing prefix: %q", message)
			}
			for _, want := range c.want {
				if !strings.Contains(message, want) {
					t.Fatalf("message %q does not name the cause (missing %q)", message, want)
				}
			}
			if strings.Contains(message, "CANARY-PASSWORD") || strings.Contains(message, "app:") {
				t.Fatalf("message leaks the credentials: %q", message)
			}
		})
	}
}
