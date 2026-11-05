package oidc_test

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"github.com/evilperson1337/requestarr/internal/auth/oidc"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// A self-contained OIDC fixture proves discovery's short-lived context is not
// accidentally retained by later exchanges/JWKS reads after a runtime swap.
func TestExchangeAfterDiscoveryContextEnds(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal("generate fixture key failed")
	}
	var base string
	encode := func(v any) string { raw, _ := json.Marshal(v); return base64.RawURLEncoding.EncodeToString(raw) }
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]any{"issuer": base, "authorization_endpoint": base + "/authorize", "token_endpoint": base + "/token", "jwks_uri": base + "/keys", "id_token_signing_alg_values_supported": []string{"RS256"}})
		case "/keys":
			_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]any{"kty": "RSA", "kid": "test-key", "use": "sig", "alg": "RS256", "n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": "AQAB"}}})
		case "/token":
			_ = r.ParseForm()
			if r.Form.Get("code_verifier") != "verifier" {
				t.Error("PKCE verifier was not sent")
			}
			client, secret, ok := r.BasicAuth()
			if !ok || client != "client" || secret != "canary-client-secret" {
				t.Error("client authentication incorrect")
			}
			header := encode(map[string]string{"alg": "RS256", "kid": "test-key"})
			claims := encode(map[string]any{"iss": base, "aud": "client", "sub": "person-1", "nonce": "nonce", "exp": time.Now().Add(time.Minute).Unix(), "iat": time.Now().Unix(), "email": "person@example.test", "name": "Person"})
			message := header + "." + claims
			sum := sha256.Sum256([]byte(message))
			sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
			if err != nil {
				t.Error("fixture signing failed")
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "canary-access-token", "token_type": "Bearer", "id_token": message + "." + base64.RawURLEncoding.EncodeToString(sig)})
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	base = srv.URL
	ctx, cancel := context.WithCancel(context.Background())
	client, err := oidc.New(ctx, oidc.Config{Issuer: base, ClientID: "client", ClientSecret: "canary-client-secret", AllowHTTP: true}, "http://localhost/api/v1/auth/callback")
	cancel()
	if err != nil {
		t.Fatal("fixture discovery failed")
	}
	identity, err := client.Exchange(context.Background(), "code", "verifier", "nonce")
	if err != nil || identity == nil || identity.Subject != "person-1" || identity.DisplayName != "Person" {
		t.Fatal("exchange after discovery context cancellation failed")
	}
	if _, err := client.Exchange(context.Background(), "code", "verifier", "wrong-nonce"); err == nil || strings.Contains(err.Error(), "canary") {
		t.Fatal("invalid nonce must fail safely")
	}
}
