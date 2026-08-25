package oidc

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"git.example.com/evilperson1337/Requestarr/internal/auth"
	"github.com/go-jose/go-jose/v4"
)

func TestLiveClientExchangeCodeValidatesSignedIDTokenClaims(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(map[string]any)
		keyID  string
		alg    jose.SignatureAlgorithm
		wantOK bool
	}{
		{name: "valid", wantOK: true},
		{name: "issuer", mutate: func(c map[string]any) { c["iss"] = "http://other.example" }},
		{name: "audience", mutate: func(c map[string]any) { c["aud"] = "another-client" }},
		{name: "multiple audiences require azp", mutate: func(c map[string]any) { c["aud"] = []string{"requestarr", "another-client"} }},
		{name: "multiple audiences reject mismatched azp", mutate: func(c map[string]any) {
			c["aud"] = []string{"requestarr", "another-client"}
			c["azp"] = "another-client"
		}},
		{name: "multiple audiences accept matching azp", mutate: func(c map[string]any) { c["aud"] = []string{"requestarr", "another-client"}; c["azp"] = "requestarr" }, wantOK: true},
		{name: "nonce", mutate: func(c map[string]any) { c["nonce"] = "another-nonce" }},
		{name: "expired", mutate: func(c map[string]any) { c["exp"] = time.Now().Add(-time.Minute).Unix() }},
		{name: "issued in future", mutate: func(c map[string]any) { c["iat"] = time.Now().Add(time.Minute).Unix() }},
		{name: "unknown key ID", keyID: "unknown"},
		{name: "wrong signing algorithm", alg: jose.HS256},
		{name: "malformed group claim", mutate: func(c map[string]any) { c["groups"] = []any{"operators", 42} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := newSignedProvider(t, tt.mutate, tt.keyID, tt.alg)
			defer provider.Close()
			config := liveTestConfig(provider.URL)
			identity, err := NewLiveClient(NewDiscoveryClient(Options{Development: true})).ExchangeCode(context.Background(), config, "authorization-code", liveTestVerifier(t), auth.Nonce("nonce"), provider.URL+"/callback")
			if tt.wantOK {
				if err != nil {
					t.Fatal(err)
				}
				if identity.Issuer != provider.URL || identity.Subject != "subject-1" || identity.DisplayName != "Ada" || len(identity.Groups) != 1 || identity.Groups[0] != "operators" {
					t.Fatalf("identity=%+v", identity)
				}
				return
			}
			if err == nil {
				t.Fatalf("ExchangeCode unexpectedly accepted %s: %+v", tt.name, identity)
			}
		})
	}
}

func TestLiveClientExchangeCodeConfidentialUsesBasicAuthAndDoesNotLeakSecret(t *testing.T) {
	const secret = "super-secret-value"
	var receivedUser, receivedPassword string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/openid-configuration" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(validMetadata("http://"+r.Host, "client_secret_basic")))
			return
		}
		if r.URL.Path == "/token" {
			receivedUser, receivedPassword, _ = r.BasicAuth()
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"invalid_client","detail":"` + secret + `"}`))
			return
		}
		t.Fatal("unexpected endpoint " + r.URL.Path)
	}))
	defer server.Close()

	config := liveTestConfig(server.URL)
	config.ClientType = "confidential"
	client := NewLiveClient(NewDiscoveryClient(Options{Development: true}), liveSecretUser(secret))
	_, err := client.ExchangeCode(context.Background(), config, "code", liveTestVerifier(t), auth.Nonce("nonce"), server.URL+"/callback")
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("error leaked secret or succeeded: %v", err)
	}
	if receivedUser != config.ClientID || receivedPassword != secret {
		t.Fatalf("basic auth user=%q password=%q", receivedUser, receivedPassword)
	}
}

func TestLiveClientBoundsEntireOperationWhenTokenBodyStalls(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(validMetadata("http://"+r.Host, "none")))
		case "/token":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			select {
			case <-r.Context().Done():
			case <-release:
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer func() { close(release); server.Close() }()
	client := NewLiveClient(NewDiscoveryClient(Options{Development: true}))
	client.operationTimeout = 25 * time.Millisecond
	started := time.Now()
	_, err := client.ExchangeCode(context.Background(), liveTestConfig(server.URL), "code", liveTestVerifier(t), auth.Nonce("nonce"), server.URL+"/callback")
	if err == nil {
		t.Fatal("stalled token response was accepted")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("operation deadline was not enforced: %s", elapsed)
	}
}

func TestLiveClientBoundsMetadataAndJWKSReads(t *testing.T) {
	tests := []struct {
		name string
		path string
		run  func(*LiveClient, auth.RuntimeConfig) error
	}{
		{name: "metadata", path: "/.well-known/openid-configuration", run: func(c *LiveClient, config auth.RuntimeConfig) error {
			_, err := c.AuthorizationURL(context.Background(), config, auth.State(liveTestOpaque(0)), auth.Nonce(liveTestOpaque(1)), liveTestVerifier(t), config.Issuer+"/callback")
			return err
		}},
		{name: "JWKS", path: "/certs", run: func(c *LiveClient, config auth.RuntimeConfig) error {
			_, err := c.ExchangeCode(context.Background(), config, "code", liveTestVerifier(t), auth.Nonce("nonce"), config.Issuer+"/callback")
			return err
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			release := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == tt.path {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					if flusher, ok := w.(http.Flusher); ok {
						flusher.Flush()
					}
					select {
					case <-r.Context().Done():
					case <-release:
					}
					return
				}
				if r.URL.Path == "/.well-known/openid-configuration" {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(validMetadata("http://"+r.Host, "none")))
					return
				}
				if r.URL.Path == "/token" {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"id_token":"invalid"}`))
					return
				}
				http.NotFound(w, r)
			}))
			defer func() { close(release); server.Close() }()
			client := NewLiveClient(NewDiscoveryClient(Options{Development: true}))
			client.operationTimeout = 25 * time.Millisecond
			started := time.Now()
			if err := tt.run(client, liveTestConfig(server.URL)); err == nil {
				t.Fatal("stalled endpoint was accepted")
			}
			if elapsed := time.Since(started); elapsed > time.Second {
				t.Fatalf("operation deadline was not enforced: %s", elapsed)
			}
		})
	}
}

type liveSecretUser string

func (s liveSecretUser) UseRuntimeClientSecret(_ context.Context, use func([]byte) error) error {
	return use([]byte(s))
}

func liveTestConfig(issuer string) auth.RuntimeConfig {
	return auth.RuntimeConfig{AuthenticationRevision: 1, Issuer: issuer, ClientID: "requestarr", ClientType: "public", Scopes: []string{"openid", "groups"}, GroupsClaim: "groups", DefaultRole: auth.RoleViewer, ActivatedAt: time.Unix(1, 0)}
}

func liveTestVerifier(t *testing.T) auth.PKCEVerifier {
	t.Helper()
	value, err := auth.ParsePKCEVerifier(strings.Repeat("A", 43))
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func liveTestOpaque(value byte) string {
	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = value
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

func newSignedProvider(t *testing.T, mutate func(map[string]any), requestedKeyID string, algorithm jose.SignatureAlgorithm) *httptest.Server {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	const validKeyID = "test-rsa-key"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		issuer := "http://" + r.Host
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(validMetadata(issuer, "none")))
		case "/certs":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: validKeyID, Algorithm: "RS256", Use: "sig"}}})
		case "/token":
			if err := r.ParseForm(); err != nil || r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("code_verifier") == "" {
				t.Fatalf("unexpected token form: %v", r.Form)
			}
			claims := map[string]any{"iss": issuer, "sub": "subject-1", "aud": "requestarr", "exp": time.Now().Add(time.Minute).Unix(), "iat": time.Now().Unix(), "nonce": "nonce", "email": "ada@example.test", "name": "Ada", "groups": []string{"operators"}}
			if mutate != nil {
				mutate(claims)
			}
			keyID := validKeyID
			if requestedKeyID != "" {
				keyID = requestedKeyID
			}
			alg := algorithm
			if alg == "" {
				alg = jose.RS256
			}
			var signingKey any = key
			if alg == jose.HS256 {
				signingKey = []byte(strings.Repeat("h", 32))
			}
			signer, signErr := jose.NewSigner(jose.SigningKey{Algorithm: alg, Key: signingKey}, (&jose.SignerOptions{}).WithHeader("kid", keyID))
			if signErr != nil {
				t.Fatal(signErr)
			}
			payload, _ := json.Marshal(claims)
			compact, signErr := signer.Sign(payload)
			if signErr != nil {
				t.Fatal(signErr)
			}
			serialized, signErr := compact.CompactSerialize()
			if signErr != nil {
				t.Fatal(signErr)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"id_token": serialized, "token_type": "Bearer"})
		default:
			http.NotFound(w, r)
		}
	}))
	return server
}
