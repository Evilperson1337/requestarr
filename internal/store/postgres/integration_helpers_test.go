//go:build integration

package postgres

import (
	"context"
	"testing"
	"time"
)

// integrationStore opens the dedicated integration database and returns a
// fresh requester to act as.
func integrationStore(t *testing.T) (context.Context, *Store, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	s, err := Open(ctx, testDatabaseURL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return ctx, s, integrationUser(t, ctx, s)
}
