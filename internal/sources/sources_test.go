package sources_test

import (
	"testing"

	"github.com/evilperson1337/requestarr/internal/inventory"
	"github.com/evilperson1337/requestarr/internal/inventory/jellyfin"
	"github.com/evilperson1337/requestarr/internal/inventory/navidrome"
	"github.com/evilperson1337/requestarr/internal/inventory/plex"
	"github.com/evilperson1337/requestarr/internal/inventory/romm"
	"github.com/evilperson1337/requestarr/internal/secrets"
	"github.com/evilperson1337/requestarr/internal/sources"
)

func descriptors(t *testing.T) map[string]inventory.Descriptor {
	t.Helper()
	jf, err := jellyfin.New("https://jellyfin.example", "k", sources.JellyfinID)
	if err != nil {
		t.Fatal(err)
	}
	px, err := plex.New("https://plex.example", "t")
	if err != nil {
		t.Fatal(err)
	}
	rm, err := romm.New("https://romm.example", "t")
	if err != nil {
		t.Fatal(err)
	}
	nd, err := navidrome.New("https://navidrome.example", "u", "p")
	if err != nil {
		t.Fatal(err)
	}
	return map[string]inventory.Descriptor{"jellyfin": jf.Descriptor(), "plex": px.Descriptor(), "romm": rm.Descriptor(), "navidrome": nd.Descriptor()}
}

// Each registered kind agrees with the adapter that implements it, so the
// registry cannot drift from the inventory clients.
func TestKindsMatchTheirAdapters(t *testing.T) {
	adapters := descriptors(t)
	for _, k := range sources.All() {
		d, ok := adapters[k.Slug]
		if !ok {
			t.Fatalf("no adapter for %s", k.Slug)
		}
		if d.ID != k.ID || d.Kind != k.Slug || d.Name != k.Name {
			t.Errorf("%s: adapter %+v disagrees with registry %+v", k.Slug, d, k)
		}
		if len(d.MediaTypes) != len(k.MediaTypes) {
			t.Errorf("%s: adapter media types %v, registry %v", k.Slug, d.MediaTypes, k.MediaTypes)
			continue
		}
		for _, m := range d.MediaTypes {
			if !k.Covers(m) {
				t.Errorf("%s: adapter covers %s, registry does not", k.Slug, m)
			}
		}
	}
	if len(adapters) != len(sources.All()) {
		t.Errorf("%d adapters for %d kinds", len(adapters), len(sources.All()))
	}
}

// Every secret a kind names exists in the secrets registry with the same environment variable.
func TestCredentialSecretsAreRegistered(t *testing.T) {
	for _, k := range sources.All() {
		for _, c := range k.Credentials {
			if c.Secret == "" {
				continue
			}
			spec, ok := secrets.Lookup(secrets.Name(c.Secret))
			if !ok {
				t.Errorf("%s: secret %q is not registered", k.Slug, c.Secret)
				continue
			}
			if spec.EnvVar != c.Env {
				t.Errorf("%s: %s env %q, secrets registry has %q", k.Slug, c.Secret, c.Env, spec.EnvVar)
			}
		}
	}
}

func TestIDsAreUniqueAndLookupWorks(t *testing.T) {
	seen := map[string]bool{}
	for _, k := range sources.All() {
		if seen[k.ID] || seen[k.Slug] {
			t.Fatalf("duplicate %s", k.Slug)
		}
		seen[k.ID], seen[k.Slug] = true, true
		if got, ok := sources.Lookup(k.Slug); !ok || got.ID != k.ID {
			t.Fatalf("Lookup(%q) = %+v", k.Slug, got)
		}
	}
	if _, ok := sources.Lookup("nope"); ok {
		t.Fatal("unknown slug found")
	}
}
