package setup

import (
	"context"
	"testing"

	"github.com/evilperson1337/requestarr/internal/config"
)

func TestSetupLibraryURLOverHTTPFollowsThePrivateHostRule(t *testing.T) {
	ctx := context.Background()
	actor := Actor{ID: "admin-1", Role: "admin"}
	var extra config.Config
	extra.SetHTTPAllowedNetworks("203.0.113.0/24,.vpn.example")
	for _, tc := range []struct {
		name        string
		url         string
		development bool
		hosts       config.PrivateHosts
		accept      bool
	}{
		{"https public", "https://plex.example.com", false, config.PrivateHosts{}, true},
		{"lan address", "http://192.168.1.20:32400", false, config.PrivateHosts{}, true},
		{"container name", "http://plex:32400", false, config.PrivateHosts{}, true},
		{"local suffix", "http://nas.local", false, config.PrivateHosts{}, true},
		{"public http", "http://plex.example.com", false, config.PrivateHosts{}, false},
		{"public address", "http://8.8.8.8:32400", false, config.PrivateHosts{}, false},
		{"path rejected", "http://192.168.1.20:32400/library", false, config.PrivateHosts{}, false},
		{"operator cidr", "http://203.0.113.9:32400", false, extra.PrivateHosts(), true},
		{"operator suffix", "http://nas.vpn.example", false, extra.PrivateHosts(), true},
		{"unlisted without operator list", "http://203.0.113.9:32400", false, config.PrivateHosts{}, false},
		{"development any host", "http://plex.example.com", true, config.PrivateHosts{}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := NewService(&memoryDrafts{}, fakeChecker{}, Runtime{Development: tc.development, PrivateHosts: tc.hosts})
			draft, err := service.Load(ctx, actor)
			if err != nil {
				t.Fatal(err)
			}
			settings := draft.Settings
			settings.Libraries.Plex.URL = tc.url
			_, err = service.Save(ctx, actor, SaveInput{ExpectedRevision: draft.Revision, CurrentStep: StepLibraries, Settings: settings})
			if tc.accept != (err == nil) {
				t.Fatalf("save %s: accept=%v, got %v", tc.url, tc.accept, err)
			}
		})
	}
}

func TestSetupPublicURLStaysHTTPSOutsideDevelopment(t *testing.T) {
	settings := NewService(&memoryDrafts{}, fakeChecker{}, Runtime{}).initial().Settings
	settings.General.PublicURL = "http://192.168.1.20:8080"
	if err := settings.Validate(Runtime{}); err == nil {
		t.Fatal("http public URL accepted on a private host")
	}
}
