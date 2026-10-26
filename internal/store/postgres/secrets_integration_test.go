//go:build integration

package postgres

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/secrets"
)

func TestStoredSecretsSetReplaceClearIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s, err := Open(ctx, testDatabaseURL(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	actor := integrationUser(t, ctx, s)
	t.Cleanup(func() {
		_, _ = s.pool.Exec(context.Background(), `DELETE FROM secret_audit_events WHERE actor_user_id=$1`, actor)
		_, _ = s.pool.Exec(context.Background(), `DELETE FROM stored_secrets`)
	})
	_, _ = s.pool.Exec(ctx, `DELETE FROM stored_secrets`)

	sealer, err := secrets.NewAESGCM(bytes.Repeat([]byte{9}, 32))
	if err != nil {
		t.Fatal(err)
	}
	svc := secrets.NewService(s, sealer, nil)
	const first, second = "first-secret-value-123", "second-secret-value-456"

	if err := svc.Set(ctx, actor, secrets.TMDBReadToken, first); err != nil {
		t.Fatal(err)
	}
	if got, _ := svc.Resolve(ctx, secrets.TMDBReadToken); got.Value != first || got.Source != secrets.SourceStored {
		t.Fatalf("after set: source %q", got.Source)
	}
	if err := svc.Set(ctx, actor, secrets.TMDBReadToken, second); err != nil {
		t.Fatal(err)
	}
	if got, _ := svc.Resolve(ctx, secrets.TMDBReadToken); got.Value != second {
		t.Fatal("replace did not take effect")
	}
	var rows int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM stored_secrets`).Scan(&rows); err != nil || rows != 1 {
		t.Fatalf("rows %d err %v", rows, err)
	}

	// Nothing the database holds may contain either value, in any column or audit row.
	for _, table := range []string{"stored_secrets", "secret_audit_events"} {
		var dump string
		if err := s.pool.QueryRow(ctx, fmt.Sprintf(`SELECT coalesce(string_agg(t::text, E'\n'), '') FROM %s t`, table)).Scan(&dump); err != nil {
			t.Fatal(err)
		}
		for _, v := range []string{first, second, "secret-value"} {
			if strings.Contains(dump, v) {
				t.Fatalf("%s contains secret material %q", table, v)
			}
		}
	}
	var keyID string
	if err := s.pool.QueryRow(ctx, `SELECT key_id FROM stored_secrets WHERE name=$1`, string(secrets.TMDBReadToken)).Scan(&keyID); err != nil || keyID == "" {
		t.Fatalf("key id %q err %v", keyID, err)
	}

	if err := svc.Clear(ctx, actor, secrets.TMDBReadToken); err != nil {
		t.Fatal(err)
	}
	if got, _ := svc.Resolve(ctx, secrets.TMDBReadToken); got.Source != secrets.SourceNone {
		t.Fatalf("after clear: source %q", got.Source)
	}
	var actions []string
	rowsQ, err := s.pool.Query(ctx, `SELECT action FROM secret_audit_events WHERE actor_user_id=$1 ORDER BY id`, actor)
	if err != nil {
		t.Fatal(err)
	}
	defer rowsQ.Close()
	for rowsQ.Next() {
		var a string
		_ = rowsQ.Scan(&a)
		actions = append(actions, a)
	}
	if strings.Join(actions, ",") != "set,set,clear" {
		t.Fatalf("audit actions %v", actions)
	}
}

func TestStoredSecretsAreUnreadableUnderAnotherKeyIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s, err := Open(ctx, testDatabaseURL(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	actor := integrationUser(t, ctx, s)
	t.Cleanup(func() {
		_, _ = s.pool.Exec(context.Background(), `DELETE FROM secret_audit_events WHERE actor_user_id=$1`, actor)
		_, _ = s.pool.Exec(context.Background(), `DELETE FROM stored_secrets`)
	})
	_, _ = s.pool.Exec(ctx, `DELETE FROM stored_secrets`)
	a, _ := secrets.NewAESGCM(bytes.Repeat([]byte{1}, 32))
	b, _ := secrets.NewAESGCM(bytes.Repeat([]byte{2}, 32))
	if err := secrets.NewService(s, a, nil).Set(ctx, actor, secrets.TMDBReadToken, "value-1"); err != nil {
		t.Fatal(err)
	}
	for name, sealer := range map[string]secrets.Sealer{"wrong key": b, "no key": nil} {
		got, err := secrets.NewService(s, sealer, nil).Resolve(ctx, secrets.TMDBReadToken)
		if err != nil || got.Source != secrets.SourceUnreadable || got.Value != "" {
			t.Errorf("%s: source %q err %v", name, got.Source, err)
		}
	}
}
