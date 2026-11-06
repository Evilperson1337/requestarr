//go:build integration

package postgres

import (
	"bytes"
	"context"
	"github.com/evilperson1337/requestarr/internal/authsettings"
	"github.com/evilperson1337/requestarr/internal/secrets"
	"testing"
)

func TestOIDCSettingsPersistAtomicallyIntegration(t *testing.T) {
	ctx, s, actor := integrationStore(t)
	t.Cleanup(func() { _ = s.SaveOIDC(context.Background(), actor, nil) })
	sealer, _ := secrets.NewAESGCM(bytes.Repeat([]byte{3}, 32))
	sealed, _ := sealer.Seal(secrets.OIDCClientSecret, []byte("oidc-canary-secret"))
	record := &authsettings.Record{Settings: authsettings.Settings{Enabled: true, ProviderName: "Keycloak", Issuer: "https://id.test", ClientID: "first"}, Secret: &secrets.Stored{Name: secrets.OIDCClientSecret, Sealed: sealed}}
	if err := s.SaveOIDC(ctx, actor, record); err != nil {
		t.Fatal("could not save OIDC settings")
	}
	got, err := s.LoadOIDC(ctx)
	if err != nil || got == nil || got.Settings != record.Settings || got.Secret == nil {
		t.Fatal("round trip lost metadata or secret")
	}
	plain, err := sealer.Open(secrets.OIDCClientSecret, got.Secret.Sealed)
	if err != nil || string(plain) != "oidc-canary-secret" {
		t.Fatal("encrypted secret did not round trip")
	}
	changed := *record
	changed.Settings.ClientID = "second"
	if err := s.SaveOIDC(ctx, "ffffffff-ffff-4fff-8fff-ffffffffffff", &changed); err == nil {
		t.Fatal("missing actor must fail audit and roll back")
	}
	got, err = s.LoadOIDC(ctx)
	if err != nil || got.Settings.ClientID != "first" {
		t.Fatal("audit failure persisted partial change")
	}
	if err := s.SaveOIDC(ctx, actor, nil); err != nil {
		t.Fatal("discard failed")
	}
	got, err = s.LoadOIDC(ctx)
	if err != nil || got != nil {
		t.Fatal("discard left metadata behind")
	}
	rows, err := s.ListStoredSecrets(ctx)
	if err != nil {
		t.Fatal("could not list secrets")
	}
	for _, r := range rows {
		if r.Name == secrets.OIDCClientSecret {
			t.Fatal("discard left secret behind")
		}
	}
}
