package musicbrainz

import "testing"

func TestPickReleasePrefersTheNamedEditionThenTheEarliestOfficial(t *testing.T) {
	releases := []groupRelease{
		{ID: pressB, Status: "Official", Date: "2001-01-01"},
		{ID: pressA, Status: "Official", Date: "2000-10-02"},
		{ID: "77777777-7777-4777-8777-777777777777", Status: "Bootleg"},
		{ID: "not-an-mbid", Status: "Official", Date: "1990-01-01"},
	}
	if got := pickRelease(releases, pressB); got != pressB {
		t.Fatalf("named edition = %q", got)
	}
	if got := pickRelease(releases, ""); got != pressA {
		t.Fatalf("earliest official = %q", got)
	}
	if got := pickRelease([]groupRelease{{ID: "77777777-7777-4777-8777-777777777777", Status: "Bootleg"}}, ""); got == "" {
		t.Fatal("falls back to the first valid release")
	}
	if got := pickRelease(nil, ""); got != "" {
		t.Fatalf("no releases = %q", got)
	}
}
