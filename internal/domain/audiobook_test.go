package domain

import (
	"net/url"
	"testing"
)

func TestParseASIN(t *testing.T) {
	good := map[string]string{
		"B002V0QK4C": "B002V0QK4C", " b002v0qk4c\n": "B002V0QK4C", "0316769487": "0316769487", "031676948x": "031676948X",
	}
	for in, want := range good {
		if got, err := ParseASIN(in); err != nil || got != want {
			t.Errorf("ParseASIN(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "B002V0QK4", "B002V0QK4CC", "B002V0QK4-", "C002V0QK4C", "03167694X7", "031676948Y", "B002 0QK4C", "B002V0QK4É"} {
		if got, err := ParseASIN(in); err == nil {
			t.Errorf("ParseASIN(%q) = %q; want error", in, got)
		}
	}
}

func TestParseAudibleURL(t *testing.T) {
	good := []struct {
		in     string
		region AudibleRegion
	}{
		{"https://www.audible.com/pd/Project-Hail-Mary-Audiobook/B08G9PRS1K", RegionUS},
		{"https://audible.com/pd/B08G9PRS1K", RegionUS},
		{"https://www.audible.co.uk/pd/Slug/b08g9prs1k/?qid=1&source_code=X#frag", RegionUK},
		{"https://www.audible.com.au/pd/Slug/B08G9PRS1K/", RegionAU},
		{"https://www.audible.de/pd/B08G9PRS1K", RegionDE},
	}
	for _, tc := range good {
		u, _ := url.Parse(tc.in)
		asin, region, err := ParseAudibleURL(u)
		if err != nil || asin != "B08G9PRS1K" || region != tc.region {
			t.Errorf("ParseAudibleURL(%q) = %q, %q, %v", tc.in, asin, region, err)
		}
	}
	for _, in := range []string{
		"https://www.amazon.com/dp/B08G9PRS1K", "https://www.audible.xx/pd/B08G9PRS1K", "https://evil.example/audible.com/pd/B08G9PRS1K",
		"https://www.audible.com/pd/", "https://www.audible.com/series/B08G9PRS1K", "https://www.audible.com/pd/a/b/B08G9PRS1K",
		"https://www.audible.com/pd/Slug/NOTANASIN", "ftp://www.audible.com/pd/B08G9PRS1K", "https://u:p@www.audible.com/pd/B08G9PRS1K",
		"https://www.audible.com:8443/pd/B08G9PRS1K", "https://notaudible.com/pd/B08G9PRS1K",
	} {
		u, err := url.Parse(in)
		if err != nil {
			t.Fatal(err)
		}
		if asin, _, err := ParseAudibleURL(u); err == nil {
			t.Errorf("ParseAudibleURL(%q) = %q; want error", in, asin)
		}
	}
	if _, _, err := ParseAudibleURL(nil); err == nil {
		t.Error("nil URL accepted")
	}
}

func TestAudibleRegionValid(t *testing.T) {
	if !RegionUK.Valid() || AudibleRegion("zz").Valid() || AudibleRegion("").Valid() {
		t.Fatal("region validation wrong")
	}
}

func TestAudiobookTakesNoSelectionAndNoIssueKindsYet(t *testing.T) {
	if err := (Selection{Seasons: []int{1}}).Validate(MediaAudiobook); err == nil {
		t.Error("audiobook accepted a selection")
	}
	if kinds := IssueKindsFor(MediaAudiobook); kinds != nil {
		t.Errorf("audiobook issue kinds are a follow-up, got %v", kinds)
	}
}
