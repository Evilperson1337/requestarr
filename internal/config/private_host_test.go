package config

import (
	"strings"
	"testing"
)

func productionLibrary(plexURL string) Config {
	return Config{Environment: "production", PlexURL: plexURL, PlexToken: "tok"}
}

func TestLibraryURLOverHTTPNeedsPrivateHostOutsideDevelopment(t *testing.T) {
	accepted := []string{
		"http://127.0.0.1:32400", "http://[::1]:32400", "http://10.1.2.3:32400", "http://172.16.0.5", "http://172.31.255.1",
		"http://192.168.1.20:32400", "http://169.254.1.1", "http://100.64.0.1", "http://100.127.255.254",
		"http://[fe80::1]:32400",
		"http://plex:32400", "http://plex.local", "http://nas.lan:32400", "http://pg.internal", "http://box.home.arpa:8096",
		"http://PLEX.LOCAL:32400", "https://plex.example.com",
	}
	for _, u := range accepted {
		if got := productionLibrary(u).LibraryProblems("plex"); len(got) != 0 {
			t.Errorf("%s: want accepted, got %v", u, got)
		}
	}
	rejected := []string{
		"http://plex.example.com", "http://8.8.8.8:32400", "http://172.32.0.1", "http://100.128.0.1",
		"http://192.169.1.1", "http://[2001:db8::1]", "http://localhost.example.com",
		"http://plex.local.example.com", "http://example.com.",
	}
	for _, u := range rejected {
		got := productionLibrary(u).LibraryProblems("plex")
		if len(got) != 1 || !strings.Contains(got[0], "https") || !strings.Contains(got[0], "REQUESTARR_HTTP_ALLOWED_NETWORKS") {
			t.Errorf("%s: want https/allowed-list message, got %v", u, got)
		}
	}
}

func TestLibraryURLOverHTTPAnyHostInDevelopment(t *testing.T) {
	c := productionLibrary("http://plex.example.com")
	c.Environment = "development"
	if got := c.LibraryProblems("plex"); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
}

func TestHTTPAllowedNetworksOnlyAdds(t *testing.T) {
	c := productionLibrary("http://203.0.113.9:32400")
	c.SetHTTPAllowedNetworks("203.0.113.0/24, .vpn.example , 10.9.0.0/16")
	if got := c.LibraryProblems("plex"); len(got) != 0 {
		t.Fatalf("CIDR: %v", got)
	}
	c.PlexURL = "http://nas.vpn.example"
	if got := c.LibraryProblems("plex"); len(got) != 0 {
		t.Fatalf("suffix: %v", got)
	}
	c.PlexURL = "http://other.example"
	if got := c.LibraryProblems("plex"); len(got) != 1 {
		t.Fatalf("unlisted: %v", got)
	}
	c.PlexURL = "http://192.168.1.1" // built-ins stay
	if got := c.LibraryProblems("plex"); len(got) != 0 {
		t.Fatalf("builtin: %v", got)
	}
}

func TestHTTPAllowedNetworksMalformedEntryNamed(t *testing.T) {
	t.Setenv("REQUESTARR_HTTP_ALLOWED_NETWORKS", "10.0.0.0/8,300.1.1.0/24,bad host")
	c := FromEnv()
	if c.HTTPAllowedNetworks == "" {
		t.Fatal("setting not read from environment")
	}
	err := c.ValidateBootstrap()
	if err == nil {
		t.Fatal("malformed entries accepted")
	}
	for _, entry := range []string{"300.1.1.0/24", "bad host"} {
		if !strings.Contains(err.Error(), "REQUESTARR_HTTP_ALLOWED_NETWORKS") || !strings.Contains(err.Error(), entry) {
			t.Errorf("entry %q not named: %v", entry, err)
		}
	}
}

func TestHTTPAllowedNetworksMalformedEntryNamedByValidate(t *testing.T) {
	c := validBase()
	c.SetHTTPAllowedNetworks("10.0.0.0/8,bad host")
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "bad host") {
		t.Fatalf("Validate did not name the malformed entry: %v", err)
	}
}

func TestPublicURLIssuerAndAppriseStayHTTPSOutsideDevelopment(t *testing.T) {
	base := validBase()
	base.Environment = "production"
	base.PublicURL = "https://app.example"
	base.SetHTTPAllowedNetworks("10.0.0.0/8")
	if err := base.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*Config){
		"public":  func(c *Config) { c.PublicURL = "http://10.0.0.5" },
		"issuer":  func(c *Config) { c.OIDCIssuer = "http://10.0.0.5/realms/x" },
		"apprise": func(c *Config) { c.AppriseURL, c.AppriseConfigKey = "http://10.0.0.5:8000", "k" },
	} {
		c := base
		mutate(&c)
		if c.Validate() == nil {
			t.Errorf("%s: http accepted on a private host", name)
		}
	}
}
