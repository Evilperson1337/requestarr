package app

import (
	"context"
	"errors"
	"testing"

	"github.com/evilperson1337/requestarr/internal/config"
	"github.com/evilperson1337/requestarr/internal/inventory"
	"github.com/evilperson1337/requestarr/internal/secrets"
	"github.com/evilperson1337/requestarr/internal/sources"
)

type stubSource struct{ id string }

func (s stubSource) Descriptor() inventory.Descriptor {
	return inventory.Descriptor{ID: s.id}
}
func (stubSource) Snapshot(context.Context) (inventory.Snapshot, error) {
	return inventory.Snapshot{}, nil
}

func fakeAdapters(built *[]string) Adapters {
	return Adapters{
		SyncStore: &fakeSyncStore{},
		Logger:    discardLogger,
		NewSource: func(_ config.Config, kind sources.Kind) (inventory.Source, sourceDecorator, error) {
			*built = append(*built, kind.Slug)
			return stubSource{id: kind.ID}, nil, nil
		},
	}
}

func TestBuildDescribesConfiguredSourcesProvidersAndSetupRuntime(t *testing.T) {
	cfg := validBase(config.Config{
		JellyfinURL: "https://jellyfin.example.test", JellyfinAPIKey: "k",
		TMDBReadToken: "t", IGDBClientID: "saved-id",
		NavidromeURL: "", // held off: its stored password is unreadable
	})
	var built []string
	rt, err := Build(booted{
		Config:                      cfg,
		Unreadable:                  map[secrets.Name]string{secrets.NavidromePassword: unreadableNote},
		IGDBClientIDFromEnvironment: false,
	}, fakeAdapters(&built))
	if err != nil {
		t.Fatal(err)
	}
	if len(built) != 1 || built[0] != "jellyfin" || len(rt.Jobs) != 1 || rt.Jobs[0].ID != sources.JellyfinID {
		t.Fatalf("built=%v jobs=%+v: only the configured Source gets a job", built, rt.Jobs)
	}
	refs := map[string]string{}
	configured := map[string]bool{}
	for _, ref := range rt.Sources {
		refs[ref.Slug] = ref.Note
		configured[ref.Slug] = ref.ID != ""
	}
	if len(rt.Sources) != 5 || !configured["jellyfin"] || configured["plex"] || configured["navidrome"] {
		t.Fatalf("source refs = %+v", rt.Sources)
	}
	if refs["navidrome"] != unreadableNote || refs["jellyfin"] != "" {
		t.Fatalf("the unreadable Navidrome secret must be noted on its Source like the others: %+v", rt.Sources)
	}
	byKey := map[string]bool{}
	for _, p := range rt.Providers {
		byKey[p.Key] = p.Configured
	}
	if !byKey["tmdb"] || byKey["igdb"] || byKey["apprise"] || !byKey["musicbrainz"] {
		t.Fatalf("providers = %+v (IGDB has an ID but no secret)", rt.Providers)
	}
	if rt.Setup.IGDBClientID != "saved-id" || rt.Setup.IGDBClientIDFromEnvironment || rt.Setup.LibraryURLs["jellyfin"] != "https://jellyfin.example.test" {
		t.Fatalf("setup runtime = %+v", rt.Setup)
	}
	for name, want := range map[secrets.Name]bool{
		secrets.TMDBReadToken: true, secrets.JellyfinAPIKey: true,
		secrets.IGDBClientSecret: false, secrets.AppriseConfigKey: false, secrets.PlexToken: false, secrets.RommToken: false, secrets.NavidromePassword: false,
	} {
		if got := rt.Setup.SecretConfigured(name); got != want {
			t.Errorf("SecretConfigured(%s) = %v, want %v", name, got, want)
		}
	}
}

// A Source with a plain username and a stored password reports both.
func TestBuildReportsNavidromeUsernameAndPasswordFromTheResolvedConfig(t *testing.T) {
	cfg := validBase(config.Config{NavidromeURL: "https://music.example.test", NavidromeUsername: "svc-music", NavidromePassword: "pw"})
	var built []string
	rt, err := Build(booted{Config: cfg}, fakeAdapters(&built))
	if err != nil {
		t.Fatal(err)
	}
	if rt.Setup.LibraryUsername("navidrome") != "svc-music" || rt.Setup.LibraryUsername("jellyfin") != "" {
		t.Fatalf("usernames = %v", rt.Setup.LibraryUsernames)
	}
	if !rt.Setup.SecretConfigured(secrets.NavidromePassword) || rt.Setup.SecretConfigured(secrets.JellyfinAPIKey) {
		t.Fatalf("secrets = %v", rt.Setup.SecretsConfigured)
	}
}

func TestBuildFailsNamingTheSourceWhenAnAdapterCannotBeBuilt(t *testing.T) {
	cfg := validBase(config.Config{PlexURL: "https://plex.example.test", PlexToken: "t"})
	_, err := Build(booted{Config: cfg}, Adapters{Logger: discardLogger, SyncStore: &fakeSyncStore{},
		NewSource: func(config.Config, sources.Kind) (inventory.Source, sourceDecorator, error) {
			return nil, nil, errors.New("boom")
		}})
	if err == nil || err.Error() != "initialize Plex integration: boom" {
		t.Fatalf("err = %v", err)
	}
}

type bootRepo struct {
	fakeSecretRepo
	savedID     string
	connections map[string]sources.Connection
}

func (r bootRepo) ListLibraryConnections(context.Context) (map[string]sources.Connection, error) {
	return r.connections, nil
}

func (r bootRepo) LoadIGDBClientID(context.Context) (string, error) { return r.savedID, nil }

// A saved client ID is read before secrets and before validation, and an
// operator-set one is remembered as such.
func TestResolveBootOrdersIGDBIDThenSecretsThenValidation(t *testing.T) {
	cfg := validBase(config.Config{SecretsKey: hexKeyA})
	b, err := resolveBoot(context.Background(), cfg, bootRepo{fakeSecretRepo: storedTMDB(t, hexKeyA, "stored-token"), savedID: "saved-client"}, discardLogger)
	if err != nil {
		t.Fatal(err)
	}
	if b.Config.IGDBClientID != "saved-client" || b.IGDBClientIDFromEnvironment || b.Config.TMDBReadToken != "stored-token" {
		t.Fatalf("booted = %+v", b)
	}
	cfg.IGDBClientID = "env-client"
	if b, err = resolveBoot(context.Background(), cfg, bootRepo{savedID: "saved-client"}, discardLogger); err != nil || !b.IGDBClientIDFromEnvironment || b.Config.IGDBClientID != "env-client" {
		t.Fatalf("booted = %+v, err = %v", b, err)
	}
	bad := validBase(config.Config{JellyfinURL: "https://jellyfin.example.test"}) // no key: validation must run last and fail
	if _, err := resolveBoot(context.Background(), bad, bootRepo{}, discardLogger); err == nil {
		t.Fatal("an invalid configuration must stop startup")
	}
}
