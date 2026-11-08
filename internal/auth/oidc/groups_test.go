package oidc_test

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/auth/oidc"
)

// exchangeWithClaims signs an ID token carrying extra claims and returns the
// identity the real client reads from it.
func exchangeWithClaims(t *testing.T, cfg oidc.Config, extra map[string]any) *oidc.Identity {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	var base string
	encode := func(v any) string { raw, _ := json.Marshal(v); return base64.RawURLEncoding.EncodeToString(raw) }
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]any{"issuer": base, "authorization_endpoint": base + "/authorize", "token_endpoint": base + "/token", "jwks_uri": base + "/keys", "id_token_signing_alg_values_supported": []string{"RS256"}})
		case "/keys":
			_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]any{"kty": "RSA", "kid": "k", "use": "sig", "alg": "RS256", "n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": "AQAB"}}})
		case "/token":
			claims := map[string]any{"iss": base, "aud": "client", "sub": "person-1", "nonce": "nonce", "exp": time.Now().Add(time.Minute).Unix(), "iat": time.Now().Unix()}
			for k, v := range extra {
				claims[k] = v
			}
			message := encode(map[string]string{"alg": "RS256", "kid": "k"}) + "." + encode(claims)
			sum := sha256.Sum256([]byte(message))
			sig, _ := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "a", "token_type": "Bearer", "id_token": message + "." + base64.RawURLEncoding.EncodeToString(sig)})
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	base = srv.URL
	cfg.Issuer, cfg.ClientID, cfg.ClientSecret, cfg.AllowHTTP = base, "client", "secret", true
	client, err := oidc.New(context.Background(), cfg, "http://localhost/api/v1/auth/callback")
	if err != nil {
		t.Fatal(err)
	}
	id, err := client.Exchange(context.Background(), "code", "v", "nonce")
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestTheIdentitySaysWhetherTheGroupsClaimWasThere(t *testing.T) {
	missing := exchangeWithClaims(t, oidc.Config{}, map[string]any{"email": "a@example.test"})
	if missing.GroupsClaimFound {
		t.Fatal("found a groups claim that is not there")
	}
	want := []string{"aud", "email", "exp", "iat", "iss", "nonce", "sub"}
	if !reflect.DeepEqual(missing.ClaimNames, want) {
		t.Fatalf("claim names = %v, want %v", missing.ClaimNames, want)
	}
	if !exchangeWithClaims(t, oidc.Config{}, map[string]any{"groups": []string{}}).GroupsClaimFound {
		t.Fatal("an empty groups claim is still a groups claim")
	}
}

func TestGroupsAreReadFromTheConfiguredIDTokenClaim(t *testing.T) {
	cases := []struct {
		name  string
		cfg   oidc.Config
		extra map[string]any
		want  []string
	}{
		{"a list under the default claim", oidc.Config{}, map[string]any{"groups": []string{"owner", "/staff"}}, []string{"owner", "/staff"}},
		{"a single text value is one group", oidc.Config{}, map[string]any{"groups": "admin"}, []string{"admin"}},
		{"a custom claim name", oidc.Config{GroupsClaim: "roles"}, map[string]any{"roles": []string{"viewer"}, "groups": []string{"owner"}}, []string{"viewer"}},
		{"the claim is missing", oidc.Config{}, map[string]any{}, nil},
		{"a claim of the wrong type gives no groups", oidc.Config{}, map[string]any{"groups": 7}, nil},
		{"non-text list items are skipped", oidc.Config{}, map[string]any{"groups": []any{"user", 3, nil}}, []string{"user"}},
		{"a dotted name is a path to Keycloak's realm roles", oidc.Config{GroupsClaim: "realm_access.roles"}, map[string]any{"realm_access": map[string]any{"roles": []string{"admin", "offline_access"}}}, []string{"admin", "offline_access"}},
		{"a dotted path to a client's roles", oidc.Config{GroupsClaim: "resource_access.requestarr.roles"}, map[string]any{"resource_access": map[string]any{"requestarr": map[string]any{"roles": []string{"owner"}}}}, []string{"owner"}},
		{"a claim named with dots wins over the path", oidc.Config{GroupsClaim: "https://example.test/groups"}, map[string]any{"https://example.test/groups": []string{"user"}}, []string{"user"}},
		{"a path through a non-object gives no groups", oidc.Config{GroupsClaim: "realm_access.roles"}, map[string]any{"realm_access": "admin"}, nil},
	}
	for _, c := range cases {
		got := exchangeWithClaims(t, c.cfg, c.extra).Groups
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
