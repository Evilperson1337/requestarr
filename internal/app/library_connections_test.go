package app

import (
	"context"
	"testing"

	"github.com/evilperson1337/requestarr/internal/config"
	"github.com/evilperson1337/requestarr/internal/sources"
)

func TestSavedConnectionFillsAMissingUrlButNeverOverridesTheEnvironment(t *testing.T) {
	repo := bootRepo{connections: map[string]sources.Connection{
		"plex":      {Slug: "plex", URL: "https://plex.example.test"},
		"jellyfin":  {Slug: "jellyfin", URL: "https://saved-jellyfin.example.test"},
		"navidrome": {Slug: "navidrome", URL: "https://music.example.test", Username: "saved-user"},
	}}
	cfg, origins, err := resolveLibraryConnections(context.Background(), config.Config{JellyfinURL: "https://env-jellyfin.example.test"}, repo)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PlexURL != "https://plex.example.test" || cfg.NavidromeURL != "https://music.example.test" || cfg.NavidromeUsername != "saved-user" {
		t.Fatalf("saved connections not applied: %+v", cfg)
	}
	if cfg.JellyfinURL != "https://env-jellyfin.example.test" || !origins["jellyfin"].FromEnvironment || !origins["jellyfin"].Saved {
		t.Fatalf("the environment URL must win: %+v %+v", cfg.JellyfinURL, origins["jellyfin"])
	}
	if origins["romm"].Saved || origins["plex"].FromEnvironment {
		t.Fatalf("origins = %+v", origins)
	}
}

func TestAnInvalidSavedConnectionIsIgnoredNotFatal(t *testing.T) {
	repo := bootRepo{connections: map[string]sources.Connection{"plex": {Slug: "plex", URL: "ftp://nope"}}}
	cfg, _, err := resolveLibraryConnections(context.Background(), config.Config{}, repo)
	if err != nil || cfg.PlexURL != "" {
		t.Fatalf("cfg %+v err %v", cfg.PlexURL, err)
	}
}
