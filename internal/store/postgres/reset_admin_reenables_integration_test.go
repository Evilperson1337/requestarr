//go:build integration

package postgres

import (
	"testing"
	"time"
)

// The local administrator is the recovery path, so a disabled account must not
// be able to lock an instance out for good. Shell access is the gate: both ways
// the reset command reaches the credential turn the account back on.
func TestResettingTheLocalAdministratorTurnsADisabledAccountBackOnIntegration(t *testing.T) {
	ctx, store := freshStore(t)
	now := time.Now().UTC()
	first, err := fastHasher.Hash("first-password-123")
	if err != nil {
		t.Fatalf("setup failed (%T)", err)
	}
	if created, err := store.CreateLocalAdmin(ctx, first, now); err != nil || !created {
		t.Fatalf("setup failed (created=%v err=%T)", created, err)
	}
	resets := map[string]func(hash string) error{
		"ResetOrCreateLocalAdmin": func(h string) error { _, err := store.ResetOrCreateLocalAdmin(ctx, h, now); return err },
		"ResetLocalAdminPassword": func(h string) error { return store.ResetLocalAdminPassword(ctx, h, now) },
	}
	for name, reset := range resets {
		if _, err := store.pool.Exec(ctx, `UPDATE local_credentials SET disabled=true`); err != nil {
			t.Fatalf("%s: could not disable the account (%T)", name, err)
		}
		next, err := fastHasher.Hash("reset-password-for-" + name)
		if err != nil {
			t.Fatalf("setup failed (%T)", err)
		}
		if err := reset(next); err != nil {
			t.Fatalf("%s failed (%T)", name, err)
		}
		status, err := store.LocalAdminStatus(ctx)
		if err != nil || !status.Exists || status.Disabled {
			t.Fatalf("%s left the account disabled (exists=%v disabled=%v err=%T)", name, status.Exists, status.Disabled, err)
		}
		if n := count(t, ctx, store, `SELECT count(*) FROM local_credentials WHERE must_change AND NOT disabled`); n != 1 {
			t.Fatalf("%s must force a password change at the next sign-in", name)
		}
	}
}
