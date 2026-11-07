package authsettings_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/evilperson1337/requestarr/internal/auth/oidc"
	"github.com/evilperson1337/requestarr/internal/authsettings"
	"github.com/evilperson1337/requestarr/internal/secrets"
)

// advertising is a provider whose discovery document lists scopes.
type advertising struct {
	provider
	scopes []string
	known  bool
}

func (a *advertising) AdvertisedScopes() ([]string, bool) { return a.scopes, a.known }

func scopeService(t *testing.T, repo *memory, factory authsettings.Factory) *authsettings.Service {
	t.Helper()
	sealer, _ := secrets.NewAESGCM(bytes.Repeat([]byte{9}, 32))
	svc, err := authsettings.New(context.Background(), repo, sealer, authsettings.Values{}, "https://app.test", factory)
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func scopeUpdate(scopes string) authsettings.Update {
	return authsettings.Update{Settings: authsettings.Settings{Enabled: true, ProviderName: "Keycloak", Issuer: "https://id.test", ClientID: "id", Scopes: scopes}, ClientSecret: "scope-canary"}
}

func TestANewSetupDefaultsToRequestingGroups(t *testing.T) {
	svc := scopeService(t, &memory{}, nil)
	st, _ := svc.Status(context.Background())
	if st.Effective.Scopes != "openid profile email groups" {
		t.Fatalf("default scopes = %q", st.Effective.Scopes)
	}
}

func TestSavedScopesAreNormalisedStoredAndHandedToTheProvider(t *testing.T) {
	ctx := context.Background()
	for in, want := range map[string]string{
		"profile  groups":         "openid profile groups",
		"groups openid groups":    "openid groups",
		"":                        "openid",
		"   ":                     "openid",
		"openid profile email":    "openid profile email",
		"offline_access custom:x": "openid offline_access custom:x",
	} {
		repo := &memory{}
		var got string
		svc := scopeService(t, repo, func(_ context.Context, v authsettings.Values, _ string) (oidc.Provider, error) {
			got = v.Scopes
			return &provider{}, nil
		})
		if err := svc.Save(ctx, "admin", scopeUpdate(in)); err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if got != want || repo.record.Settings.Scopes != want {
			t.Errorf("%q: provider got %q, stored %q, want %q", in, got, repo.record.Settings.Scopes, want)
		}
		if st, _ := svc.Status(ctx); st.Effective.Scopes != want {
			t.Errorf("%q: status shows %q", in, st.Effective.Scopes)
		}
	}
}

func TestScopesWithUnsafeCharactersAreRefused(t *testing.T) {
	for _, in := range []string{`a"b`, "a\\b", "a\tb\x00", "café"} {
		repo := &memory{}
		svc := scopeService(t, repo, func(context.Context, authsettings.Values, string) (oidc.Provider, error) { return &provider{}, nil })
		if err := svc.Save(context.Background(), "admin", scopeUpdate(in)); !errors.Is(err, authsettings.ErrInvalid) || repo.record != nil {
			t.Errorf("%q accepted: %v", in, err)
		}
	}
}

func TestAConfigurationSavedBeforeScopesExistedKeepsTheOldRequest(t *testing.T) {
	ctx := context.Background()
	sealer, _ := secrets.NewAESGCM(bytes.Repeat([]byte{9}, 32))
	sealed, _ := sealer.Seal(secrets.OIDCClientSecret, []byte("scope-canary"))
	repo := &memory{record: &authsettings.Record{Settings: authsettings.Settings{Enabled: true, ProviderName: "Keycloak", Issuer: "https://id.test", ClientID: "id"}, Secret: &secrets.Stored{Name: secrets.OIDCClientSecret, Sealed: sealed}}}
	var got string
	svc, err := authsettings.New(ctx, repo, sealer, authsettings.Values{}, "https://app.test", func(_ context.Context, v authsettings.Values, _ string) (oidc.Provider, error) {
		got = v.Scopes
		return &provider{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("legacy record handed scopes %q; the provider adapter must apply the legacy default", got)
	}
	if st, _ := svc.Status(ctx); st.Effective.Scopes != "openid profile email" {
		t.Fatalf("legacy effective scopes = %q", st.Effective.Scopes)
	}
}

func TestEnvironmentConfigurationShowsTheScopesItRequests(t *testing.T) {
	env := authsettings.Values{Issuer: "https://env.test", ClientID: "e", ClientSecret: "s"}
	svc, err := authsettings.New(context.Background(), &memory{}, nil, env, "https://app.test", func(context.Context, authsettings.Values, string) (oidc.Provider, error) { return &provider{}, nil })
	if err != nil {
		t.Fatal(err)
	}
	st, _ := svc.Status(context.Background())
	if !st.ReadOnly || st.Effective.Scopes != "openid profile email" {
		t.Fatalf("env readOnly=%v scopes=%q", st.ReadOnly, st.Effective.Scopes)
	}
}

func TestTestWarnsWhenGroupsIsRequestedButNotAdvertised(t *testing.T) {
	cases := []struct {
		name   string
		scopes string
		adv    *advertising
		warn   bool
	}{
		{"groups unlisted", "openid groups", &advertising{scopes: []string{"openid", "email"}, known: true}, true},
		{"groups listed", "openid groups", &advertising{scopes: []string{"openid", "groups"}, known: true}, false},
		{"provider lists nothing", "openid groups", &advertising{}, false},
		{"groups not requested", "openid email", &advertising{scopes: []string{"openid"}, known: true}, false},
	}
	for _, c := range cases {
		svc := scopeService(t, &memory{}, func(context.Context, authsettings.Values, string) (oidc.Provider, error) { return c.adv, nil })
		warnings, err := svc.Test(context.Background(), scopeUpdate(c.scopes))
		if err != nil {
			t.Fatalf("%s: Test must still pass: %v", c.name, err)
		}
		if (len(warnings) > 0) != c.warn {
			t.Errorf("%s: warnings = %v", c.name, warnings)
		}
	}
}
