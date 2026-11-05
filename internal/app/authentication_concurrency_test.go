package app_test

import (
	"context"
	"encoding/json"
	"github.com/evilperson1337/requestarr/internal/app"
	"github.com/evilperson1337/requestarr/internal/auth/oidc"
	"github.com/evilperson1337/requestarr/internal/authsettings"
	"github.com/evilperson1337/requestarr/internal/config"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestConcurrentAuthenticationSavesPublishCoherentSnapshots(t *testing.T) {
	ctx := context.Background()
	var active, max atomic.Int32
	cfg := config.Config{PublicURL: "https://app.test", SecretsKey: strings.Repeat("07", 32)}
	svc, err := app.NewAuthentication(ctx, cfg, &oidcRepo{}, func(_ context.Context, v authsettings.Values, _ string) (oidc.Provider, error) {
		n := active.Add(1)
		for {
			old := max.Load()
			if n <= old || max.CompareAndSwap(old, n) {
				break
			}
		}
		defer active.Add(-1)
		return fakeOIDC{}, nil
	})
	if err != nil {
		t.Fatal("startup failed")
	}
	var writers, readers sync.WaitGroup
	done := make(chan struct{})
	readers.Add(1)
	go func() {
		defer readers.Done()
		for {
			select {
			case <-done:
				return
			default:
			}
			snap := svc.Snapshot()
			if snap.Provider != nil && snap.Issuer != "https://one.test" && snap.Issuer != "https://two.test" {
				t.Error("torn snapshot")
			}
			_ = svc.Options()
		}
	}()
	for i := 0; i < 24; i++ {
		writers.Add(1)
		go func(i int) {
			defer writers.Done()
			issuer := "https://one.test"
			if i%2 == 1 {
				issuer = "https://two.test"
			}
			u := authsettings.Update{Settings: authsettings.Settings{Enabled: true, ProviderName: "Keycloak", Issuer: issuer, ClientID: "client"}, ClientSecret: "canary-concurrent-secret"}
			if svc.Save(ctx, "admin", u) != nil {
				t.Error("concurrent save failed")
			}
		}(i)
	}
	writers.Wait()
	close(done)
	readers.Wait()
	if max.Load() != 1 {
		t.Fatal("concurrent builds were not serialized")
	}
	st, _ := svc.Status(ctx)
	raw, err := json.Marshal(st)
	if err != nil || strings.Contains(string(raw), "canary-concurrent-secret") {
		t.Fatal("status leaked plaintext")
	}
	if !st.Working || st.Saved == nil || st.Effective.Issuer != svc.Snapshot().Issuer {
		t.Fatal("persisted and live state disagree")
	}
}
