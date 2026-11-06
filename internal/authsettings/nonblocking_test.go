package authsettings_test

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/auth/oidc"
	"github.com/evilperson1337/requestarr/internal/authsettings"
	"github.com/evilperson1337/requestarr/internal/secrets"
)

// gate lets a test hold discovery open and see when it started.
type gate struct {
	mu      sync.Mutex
	started chan string
	release chan struct{}
	calls   int
}

func newGate() *gate { return &gate{started: make(chan string, 8), release: make(chan struct{})} }

func (g *gate) factory(_ context.Context, v authsettings.Values, _ string) (oidc.Provider, error) {
	g.mu.Lock()
	g.calls++
	g.mu.Unlock()
	g.started <- v.ClientID
	<-g.release
	return &provider{name: v.ClientID}, nil
}

func within(t *testing.T, what string, d time.Duration, f func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { f(); close(done) }()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("%s waited for discovery on the network", what)
	}
}

func update(id string) authsettings.Update {
	return authsettings.Update{Settings: authsettings.Settings{Enabled: true, ProviderName: "Keycloak", Issuer: "https://issuer.test", ClientID: id}, ClientSecret: "secret-value"}
}

// S6: discovery is a network call. While one is in flight, reading the status
// and the live snapshot (the sign-in path) must not wait for it.
func TestStatusAndSnapshotDoNotWaitForADiscoveryInFlight(t *testing.T) {
	for _, op := range []string{"Test", "Save"} {
		t.Run(op, func(t *testing.T) {
			ctx := context.Background()
			sealer, _ := secrets.NewAESGCM(bytes.Repeat([]byte{7}, 32))
			g := newGate()
			svc, err := authsettings.New(ctx, &memory{}, sealer, authsettings.Values{}, "https://requestarr.test", g.factory)
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() {
				if op == "Test" {
					done <- svc.Test(ctx, update("first"))
				} else {
					done <- svc.Save(ctx, "admin", update("first"))
				}
			}()
			<-g.started
			within(t, "Status", time.Second, func() { _, _ = svc.Status(ctx) })
			within(t, "Snapshot", time.Second, func() { _ = svc.Snapshot() })
			within(t, "Options", time.Second, func() { _ = svc.Options() })
			close(g.release)
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		})
	}
}

// Writers still run one at a time, and a failed or slow rebuild keeps the
// previous snapshot until the new one is swapped in whole.
func TestWritersAreSerialisedAndTheSwapStaysAtomic(t *testing.T) {
	ctx := context.Background()
	sealer, _ := secrets.NewAESGCM(bytes.Repeat([]byte{7}, 32))
	g := newGate()
	svc, _ := authsettings.New(ctx, &memory{}, sealer, authsettings.Values{}, "https://requestarr.test", g.factory)
	first := make(chan error, 1)
	second := make(chan error, 1)
	go func() { first <- svc.Save(ctx, "admin", update("first")) }()
	<-g.started
	go func() { second <- svc.Save(ctx, "admin", update("second")) }()
	select {
	case id := <-g.started:
		t.Fatalf("a second writer began discovery (%s) while the first was still running", id)
	case <-time.After(150 * time.Millisecond):
	}
	if svc.Snapshot().Provider != nil {
		t.Fatal("a snapshot was published before discovery finished")
	}
	g.release <- struct{}{}
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if p := svc.Snapshot().Provider; p == nil || p.AuthorizationURL("", "", "") != "first" {
		t.Fatal("the first save did not publish")
	}
	<-g.started // the second now runs
	if p := svc.Snapshot().Provider; p == nil || p.AuthorizationURL("", "", "") != "first" {
		t.Fatal("the previous snapshot must stay live while the next one is being discovered")
	}
	g.release <- struct{}{}
	if err := <-second; err != nil {
		t.Fatal(err)
	}
	if p := svc.Snapshot().Provider; p == nil || p.AuthorizationURL("", "", "") != "second" {
		t.Fatal("the second save did not publish")
	}
}
