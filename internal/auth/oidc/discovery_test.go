package oidc_test

import (
	"context"
	"encoding/json"
	"github.com/evilperson1337/requestarr/internal/auth/oidc"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDiscoveryRejectsUnsafeOrUnboundedMetadata(t *testing.T) {
	for _, mode := range []string{"valid", "redirect", "oversize", "html", "bad-token", "bad-auth", "bad-jwks", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			var base string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.UserAgent() == "" {
					t.Error("missing user-agent")
				}
				if mode == "redirect" {
					http.Redirect(w, r, "http://user:canary-secret@localhost", 302)
					return
				}
				if mode == "html" {
					w.Header().Set("Content-Type", "text/html")
					_, _ = w.Write([]byte("canary-secret"))
					return
				}
				w.Header().Set("Content-Type", "application/json")
				if mode == "oversize" {
					_, _ = w.Write([]byte(strings.Repeat("x", (1<<20)+1)))
					return
				}
				data := map[string]any{"issuer": base, "authorization_endpoint": base + "/authorize", "token_endpoint": base + "/token", "jwks_uri": base + "/keys", "id_token_signing_alg_values_supported": []string{"RS256"}}
				for key, target := range map[string]string{"bad-token": "token_endpoint", "bad-auth": "authorization_endpoint", "bad-jwks": "jwks_uri"} {
					if mode == key {
						data[target] = "http://user:canary-secret@external.test/path"
					}
				}
				_ = json.NewEncoder(w).Encode(data)
			}))
			defer srv.Close()
			base = srv.URL
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if mode == "cancelled" {
				cancel()
			}
			_, err := oidc.New(ctx, oidc.Config{Issuer: base, ClientID: "client", ClientSecret: "canary-secret", AllowHTTP: true}, "http://localhost/api/v1/auth/callback")
			if mode == "valid" {
				if err != nil {
					t.Fatal("valid discovery rejected")
				}
			} else if err == nil || strings.Contains(err.Error(), "canary-secret") {
				t.Fatal("unsafe discovery must fail without exposing credentials")
			}
		})
	}
	_, err := oidc.New(context.Background(), oidc.Config{Issuer: "http://external.test", ClientID: "client", ClientSecret: "secret", AllowHTTP: true}, "https://app.test/api/v1/auth/callback")
	if err == nil {
		t.Fatal("development HTTP must be loopback only")
	}
}
