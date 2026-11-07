//go:build integration

package postgres

import (
	"testing"

	"github.com/evilperson1337/requestarr/db/migrations"
)

// An upgrade must change nothing about the sign-in request: a saved OIDC
// configuration gets exactly the scopes it asked for before the setting existed.
func TestOIDCScopesMigrationKeepsTheExistingRequestIntegration(t *testing.T) {
	ctx, s := settingsStore(t)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM app_settings WHERE key=$1`, settingOIDC); err != nil {
		t.Fatal(err)
	}
	up, err := migrations.FS.ReadFile("000039_oidc_scopes.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	down, err := migrations.FS.ReadFile("000039_oidc_scopes.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	scopes := func() (string, bool) {
		var v *string
		err := tx.QueryRow(ctx, `SELECT (SELECT (value::jsonb)->>'scopes' FROM app_settings WHERE key=$1)`, settingOIDC).Scan(&v)
		if err != nil {
			t.Fatal(err)
		}
		if v == nil {
			return "", false
		}
		return *v, true
	}

	// No saved configuration: nothing is created.
	if _, err := tx.Exec(ctx, string(up)); err != nil {
		t.Fatal(err)
	}
	var rows int
	_ = tx.QueryRow(ctx, `SELECT count(*) FROM app_settings WHERE key=$1`, settingOIDC).Scan(&rows)
	if rows != 0 {
		t.Fatal("the migration created an OIDC configuration")
	}

	// A saved configuration without scopes keeps the legacy request, other fields untouched.
	saved := `{"enabled":true,"providerName":"Keycloak","issuer":"https://id.test","clientId":"id"}`
	if _, err := tx.Exec(ctx, `INSERT INTO app_settings(key,value) VALUES($1,$2)`, settingOIDC, saved); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, string(up)); err != nil {
		t.Fatal(err)
	}
	if got, ok := scopes(); !ok || got != "openid profile email" {
		t.Fatalf("scopes after upgrade = %q (set=%v)", got, ok)
	}
	var clientID string
	_ = tx.QueryRow(ctx, `SELECT (value::jsonb)->>'clientId' FROM app_settings WHERE key=$1`, settingOIDC).Scan(&clientID)
	if clientID != "id" {
		t.Fatal("the migration changed other fields")
	}

	// Scopes an operator already chose are never overwritten.
	if _, err := tx.Exec(ctx, `UPDATE app_settings SET value='{"clientId":"id","scopes":"openid groups"}' WHERE key=$1`, settingOIDC); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, string(up)); err != nil {
		t.Fatal(err)
	}
	if got, _ := scopes(); got != "openid groups" {
		t.Fatalf("chosen scopes overwritten: %q", got)
	}

	// Rolling back removes the field again.
	if _, err := tx.Exec(ctx, string(down)); err != nil {
		t.Fatal(err)
	}
	if _, ok := scopes(); ok {
		t.Fatal("down migration left scopes behind")
	}
}
