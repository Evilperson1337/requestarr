package authsettings_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/evilperson1337/requestarr/internal/auth/oidc"
	"github.com/evilperson1337/requestarr/internal/authsettings"
	"github.com/evilperson1337/requestarr/internal/secrets"
	"strings"
	"testing"
)

type memory struct {
	record *authsettings.Record
	fail   bool
	saves  int
}

func (m *memory) LoadOIDC(context.Context) (*authsettings.Record, error) { return m.record, nil }
func (m *memory) SaveOIDC(_ context.Context, _ string, r *authsettings.Record) error {
	if m.fail {
		return errors.New("storage failed")
	}
	m.record = r
	m.saves++
	return nil
}

type provider struct{ name string }

func (p *provider) AuthorizationURL(string, string, string) string { return p.name }
func (*provider) Exchange(context.Context, string, string, string) (*oidc.Identity, error) {
	return &oidc.Identity{Subject: "person"}, nil
}
func TestSavePublishesOnlyDiscoveredAndDurableConfiguration(t *testing.T) {
	ctx := context.Background()
	repo := &memory{}
	sealer, _ := secrets.NewAESGCM(bytes.Repeat([]byte{7}, 32))
	factory := func(_ context.Context, v authsettings.Values, _ string) (oidc.Provider, error) {
		if v.ClientID == "bad" {
			return nil, errors.New("canary-secret from upstream")
		}
		return &provider{name: v.ClientID}, nil
	}
	svc, err := authsettings.New(ctx, repo, sealer, authsettings.Values{}, "https://requestarr.test", factory)
	if err != nil || svc.Snapshot().Provider != nil {
		t.Fatal("no-OIDC startup must work")
	}
	good := authsettings.Update{Settings: authsettings.Settings{Enabled: true, ProviderName: "Keycloak", Issuer: "https://issuer.test", ClientID: "good"}, ClientSecret: "canary-secret"}
	if err := svc.Save(ctx, "admin", good); err != nil {
		t.Fatal("save should discover and publish")
	}
	old := svc.Snapshot()
	if old.Provider == nil || old.Issuer != "https://issuer.test" {
		t.Fatal("missing live snapshot")
	}
	if bytes.Contains(repo.record.Secret.Ciphertext, []byte(good.ClientSecret)) {
		t.Fatal("secret stored in plaintext")
	}
	bad := good
	bad.ClientID = "bad"
	if err := svc.Save(ctx, "admin", bad); err == nil || strings.Contains(err.Error(), good.ClientSecret) {
		t.Fatal("failed discovery must return a value-free error")
	}
	if svc.Snapshot() != old || repo.record.Settings.ClientID != "good" {
		t.Fatal("failed change replaced working state")
	}
	repo.fail = true
	good.ClientID = "second"
	if err := svc.Save(ctx, "admin", good); err == nil || svc.Snapshot() != old {
		t.Fatal("failed persistence published state")
	}
	repo.fail = false
	good.Enabled = false
	good.ClientSecret = ""
	if err := svc.Save(ctx, "admin", good); err != nil || svc.Snapshot().Provider != nil {
		t.Fatal("disable must retain configuration and switch off")
	}
	if err := svc.SetEnabled(ctx, "admin", true); err != nil || svc.Snapshot().Provider == nil {
		t.Fatal("enable must discover saved values")
	}
	if old.Provider.AuthorizationURL("", "", "") != "good" {
		t.Fatal("old snapshot mutated")
	}
	if err := svc.Discard(ctx, "admin"); err != nil || repo.record != nil || svc.Snapshot().Provider != nil {
		t.Fatal("discard did not clear saved state")
	}
}

func TestMissingKeyAndUnreadableSecretDoNotBlockDisabling(t *testing.T) {
	ctx := context.Background()
	repo := &memory{}
	svc, _ := authsettings.New(ctx, repo, nil, authsettings.Values{}, "https://app.test", nil)
	st, _ := svc.Status(ctx)
	if st.CanSaveSecret {
		t.Fatal("missing key must be reported")
	}
	u := authsettings.Update{Settings: authsettings.Settings{Enabled: false, ProviderName: "Keycloak", Issuer: "https://id.test", ClientID: "id"}, ClientSecret: "canary-secret"}
	if err := svc.Save(ctx, "admin", u); !errors.Is(err, secrets.ErrNoMasterKey) {
		t.Fatal("secret save requires master key")
	}
	sealer, _ := secrets.NewAESGCM(bytes.Repeat([]byte{8}, 32))
	sealed, _ := sealer.Seal(secrets.OIDCClientSecret, []byte(u.ClientSecret))
	repo.record = &authsettings.Record{Settings: u.Settings, Secret: &secrets.Stored{Name: secrets.OIDCClientSecret, Sealed: sealed}}
	repo.record.Settings.Enabled = true
	svc, _ = authsettings.New(ctx, repo, nil, authsettings.Values{}, "https://app.test", nil)
	st, _ = svc.Status(ctx)
	if !st.SecretUnreadable || st.Working {
		t.Fatal("unreadable saved secret must fail closed")
	}
	if err := svc.SetEnabled(ctx, "admin", false); err != nil {
		t.Fatal("must be able to disable with lost master key")
	}
	if repo.record.Settings.Enabled {
		t.Fatal("disable did not persist")
	}
}

func TestEnvironmentOverrideAndDiscoveryTestNeverMutateSavedState(t *testing.T) {
	ctx := context.Background()
	repo := &memory{}
	sealer, _ := secrets.NewAESGCM(bytes.Repeat([]byte{4}, 32))
	sealed, _ := sealer.Seal(secrets.OIDCClientSecret, []byte("saved-canary"))
	record := &authsettings.Record{Settings: authsettings.Settings{Enabled: true, ProviderName: "Saved", Issuer: "https://saved.test", ClientID: "saved-id"}, Secret: &secrets.Stored{Name: secrets.OIDCClientSecret, Sealed: sealed}}
	for _, complete := range []bool{false, true} {
		repo.record = record
		env := authsettings.Values{ClientID: "environment-id"}
		if complete {
			env.Issuer = "https://environment.test"
			env.ClientSecret = "env-canary"
		}
		calls := 0
		svc, err := authsettings.New(ctx, repo, sealer, env, "https://app.test", func(_ context.Context, v authsettings.Values, _ string) (oidc.Provider, error) {
			calls++
			if v != env {
				t.Error("environment mixed with saved values")
			}
			return &provider{name: "environment"}, nil
		})
		if err != nil {
			t.Fatal("startup failed")
		}
		st, _ := svc.Status(ctx)
		if !st.ReadOnly || len(st.Ignored) != 3 || len(st.Differing) != 3 {
			t.Fatal("missing unit conflicts")
		}
		if !complete && (calls != 0 || st.State != "environment_incomplete" || len(st.Missing) != 2) {
			t.Fatal("incomplete environment must not use saved values")
		}
		old := svc.Snapshot()
		if err := svc.Save(ctx, "admin", authsettings.Update{}); !errors.Is(err, authsettings.ErrReadOnly) {
			t.Fatal("environment must be read-only")
		}
		if err := svc.SetEnabled(ctx, "admin", false); !errors.Is(err, authsettings.ErrReadOnly) {
			t.Fatal("environment enable switch must be read-only")
		}
		if err := svc.Discard(ctx, "admin"); err != nil || repo.record != nil || svc.Snapshot() != old {
			t.Fatal("discard must not change environment provider")
		}
	}
	// Testing a candidate does not save it or issue a reusable enable permission.
	repo.record = nil
	svc, _ := authsettings.New(ctx, repo, sealer, authsettings.Values{}, "https://app.test", func(context.Context, authsettings.Values, string) (oidc.Provider, error) {
		return nil, errors.New("canary-secret")
	})
	u := authsettings.Update{Settings: record.Settings, ClientSecret: "saved-canary"}
	if err := svc.Test(ctx, u); err == nil || strings.Contains(err.Error(), u.ClientSecret) {
		t.Fatal("failed test must be safe")
	}
	if repo.record != nil || svc.Snapshot().Provider != nil {
		t.Fatal("test mutated configuration")
	}
	if err := svc.SetEnabled(ctx, "admin", true); err == nil {
		t.Fatal("test must not allow enabling missing config")
	}
}

func TestRepeatedSaveIsIdempotent(t *testing.T) {
	ctx := context.Background()
	repo := &memory{}
	sealer, _ := secrets.NewAESGCM(bytes.Repeat([]byte{9}, 32))
	svc, _ := authsettings.New(ctx, repo, sealer, authsettings.Values{}, "https://app.test", func(context.Context, authsettings.Values, string) (oidc.Provider, error) { return &provider{}, nil })
	u := authsettings.Update{Settings: authsettings.Settings{Enabled: true, ProviderName: "Keycloak", Issuer: "https://id.test", ClientID: "id"}, ClientSecret: "canary-secret"}
	if svc.Save(ctx, "admin", u) != nil || svc.Save(ctx, "admin", u) != nil {
		t.Fatal("save failed")
	}
	if repo.saves != 1 {
		t.Fatal("identical save must not duplicate persistence or audit")
	}
}

func TestFailedTestAndEnablePreserveActiveAndDisabledSettings(t *testing.T) {
	ctx := context.Background()
	repo := &memory{}
	sealer, _ := secrets.NewAESGCM(bytes.Repeat([]byte{6}, 32))
	factory := func(_ context.Context, v authsettings.Values, _ string) (oidc.Provider, error) {
		if v.ClientID == "broken" {
			return nil, errors.New("upstream canary-secret")
		}
		return &provider{name: v.ClientID}, nil
	}
	svc, _ := authsettings.New(ctx, repo, sealer, authsettings.Values{}, "https://app.test", factory)
	u := authsettings.Update{Settings: authsettings.Settings{Enabled: true, ProviderName: "Keycloak", Issuer: "https://id.test", ClientID: "good"}, ClientSecret: "canary-secret"}
	if svc.Save(ctx, "admin", u) != nil {
		t.Fatal("save failed")
	}
	old := svc.Snapshot()
	restarted, err := authsettings.New(ctx, repo, sealer, authsettings.Values{}, "https://app.test", factory)
	if err != nil || !restarted.Options().OIDCEnabled {
		t.Fatal("saved-only configuration did not survive restart")
	}
	u.ClientID = "broken"
	if svc.Test(ctx, u) == nil || svc.Snapshot() != old || repo.record.Settings.ClientID != "good" {
		t.Fatal("failed diagnostic test must retain working state")
	}
	u.Enabled = false
	if svc.Save(ctx, "admin", u) != nil {
		t.Fatal("disabled candidate should be storable before discovery")
	}
	if svc.SetEnabled(ctx, "admin", true) == nil || svc.Options().OIDCEnabled || repo.record.Settings.Enabled {
		t.Fatal("undiscovered candidate must not become enabled")
	}
}

// S5: whatever the adapter or network says, the message shown is one fixed
// phrase; addresses, hostnames and response bodies never reach the page.
func TestDiscoveryMessageCollapsesUnknownErrors(t *testing.T) {
	leaky := errors.New("dial tcp 10.1.2.3:443: connect: connection refused; body: canary-secret")
	got := authsettings.DiscoveryMessage(leaky)
	if got != authsettings.ErrDiscovery.Error() || strings.Contains(got, "10.1.2.3") || strings.Contains(got, "canary") {
		t.Fatalf("message %q leaks the underlying error", got)
	}
	wrapped := fmt.Errorf("%w: dial tcp 10.1.2.3:443", oidc.ErrIssuerURL)
	if m := authsettings.DiscoveryMessage(wrapped); strings.Contains(m, "10.1.2.3") || !strings.Contains(m, oidc.ErrIssuerURL.Error()) {
		t.Fatalf("a wrapped known category must show only its fixed phrase: %q", m)
	}
}
