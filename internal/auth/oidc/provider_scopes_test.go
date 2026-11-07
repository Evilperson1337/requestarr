package oidc_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"

	"github.com/evilperson1337/requestarr/internal/auth/oidc"
)

func scopeServer(t *testing.T, advertised any) string {
	t.Helper()
	var base string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		doc := map[string]any{"issuer": base, "authorization_endpoint": base + "/authorize", "token_endpoint": base + "/token", "jwks_uri": base + "/keys", "id_token_signing_alg_values_supported": []string{"RS256"}}
		if advertised != nil {
			doc["scopes_supported"] = advertised
		}
		_ = json.NewEncoder(w).Encode(doc)
	}))
	t.Cleanup(srv.Close)
	base = srv.URL
	return base
}

func requestedScope(t *testing.T, scopes []string) string {
	t.Helper()
	base := scopeServer(t, nil)
	c, err := oidc.New(context.Background(), oidc.Config{Issuer: base, ClientID: "c", ClientSecret: "s", Scopes: scopes, AllowHTTP: true}, "http://localhost/cb")
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(c.AuthorizationURL("state", "nonce", "challenge"))
	if err != nil {
		t.Fatal(err)
	}
	return u.Query().Get("scope")
}

func TestAuthorizationRequestSendsTheConfiguredScopes(t *testing.T) {
	if got := requestedScope(t, []string{"openid", "profile", "email", "groups"}); got != "openid profile email groups" {
		t.Fatalf("scope = %q", got)
	}
}

func TestAuthorizationRequestAlwaysAsksForOpenID(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want string
	}{
		{"nil", nil, "openid"},
		{"empty", []string{}, "openid"},
		{"missing", []string{"groups"}, "openid groups"},
		{"duplicate", []string{"groups", "openid", "groups"}, "openid groups"},
	}
	for _, c := range cases {
		if got := requestedScope(t, c.in); got != c.want {
			t.Errorf("%s: scope = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestClientReportsTheScopesTheProviderAdvertises(t *testing.T) {
	c, err := oidc.New(context.Background(), oidc.Config{Issuer: scopeServer(t, []string{"openid", "email"}), ClientID: "c", ClientSecret: "s", AllowHTTP: true}, "http://localhost/cb")
	if err != nil {
		t.Fatal(err)
	}
	got, known := c.AdvertisedScopes()
	if !known || !reflect.DeepEqual(got, []string{"openid", "email"}) {
		t.Fatalf("advertised = %v known=%v", got, known)
	}
	c, err = oidc.New(context.Background(), oidc.Config{Issuer: scopeServer(t, nil), ClientID: "c", ClientSecret: "s", AllowHTTP: true}, "http://localhost/cb")
	if err != nil {
		t.Fatal(err)
	}
	if _, known := c.AdvertisedScopes(); known {
		t.Fatal("a provider that lists no scopes must report them unknown")
	}
}
