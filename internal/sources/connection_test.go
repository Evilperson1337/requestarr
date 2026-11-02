package sources

import "testing"

func TestConnectionValidate(t *testing.T) {
	good := []Connection{
		{Slug: "plex", URL: "https://plex.example.test:32400"},
		{Slug: "jellyfin", URL: "http://10.0.0.5:8096/jellyfin"},
		{Slug: "navidrome", URL: "https://music.example.test", Username: "alice"},
	}
	for _, c := range good {
		if err := c.Validate(); err != nil {
			t.Errorf("%+v: %v", c, err)
		}
	}
	bad := map[string]Connection{
		"unknown source":             {Slug: "emby", URL: "https://x.test"},
		"not http":                   {Slug: "plex", URL: "ftp://x.test"},
		"no host":                    {Slug: "plex", URL: "https://"},
		"embedded credentials":       {Slug: "plex", URL: "https://u:p@x.test"},
		"query":                      {Slug: "plex", URL: "https://x.test/?X-Plex-Token=abc"},
		"fragment":                   {Slug: "plex", URL: "https://x.test/#a"},
		"username on a token source": {Slug: "plex", URL: "https://x.test", Username: "alice"},
		"navidrome needs one":        {Slug: "navidrome", URL: "https://x.test"},
		"padded username":            {Slug: "navidrome", URL: "https://x.test", Username: " alice"},
	}
	for name, c := range bad {
		if err := c.Validate(); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}
