package domain

import "testing"

func TestIssueKindsAreOfferedPerMediaTypeWithOtherLast(t *testing.T) {
	for _, media := range []MediaType{MediaMovie, MediaTV, MediaAlbum, MediaSong, MediaGame} {
		kinds := IssueKindsFor(media)
		if len(kinds) == 0 || kinds[len(kinds)-1] != IssueOther {
			t.Errorf("%s kinds = %v: other must be last", media, kinds)
		}
	}
	if IssueKindsFor("podcast") != nil {
		t.Error("an unknown media type has no kinds")
	}
}

func TestValidateIssue(t *testing.T) {
	season := Selection{Seasons: []int{2}}
	track := Selection{Tracks: []TrackRef{{ID: "t1", Title: "One"}}}
	ok := []struct {
		media MediaType
		kind  IssueKind
		desc  string
		part  Selection
	}{
		{MediaMovie, IssueSubtitles, "", Selection{}},
		{MediaTV, IssueVideo, "stutters", season},
		{MediaAlbum, IssueAudio, "", track},
		{MediaGame, IssueWrongRegion, "", Selection{}},
		{MediaMovie, IssueOther, "something else", Selection{}},
	}
	for _, tc := range ok {
		if err := ValidateIssue(tc.media, tc.kind, tc.desc, tc.part); err != nil {
			t.Errorf("%+v: %v", tc, err)
		}
	}
	bad := map[string]struct {
		media MediaType
		kind  IssueKind
		desc  string
		part  Selection
		want  error
	}{
		"a game has no subtitles": {MediaGame, IssueSubtitles, "", Selection{}, ErrIssueKind},
		"an album has no video":   {MediaAlbum, IssueVideo, "", Selection{}, ErrIssueKind},
		"other needs words":       {MediaTV, IssueOther, "   ", Selection{}, ErrIssueDescription},
		"too long":                {MediaTV, IssueVideo, string(make([]rune, MaxIssueDescription+1)), Selection{}, ErrIssueDescription},
		"control characters":      {MediaTV, IssueVideo, "a\x00b", Selection{}, ErrIssueDescription},
		"a movie has no seasons":  {MediaMovie, IssueVideo, "", season, ErrIssuePart},
		"a series has no tracks":  {MediaTV, IssueVideo, "", track, ErrIssuePart},
		"a game has no parts":     {MediaGame, IssueNoBoot, "", season, ErrIssuePart},
	}
	for name, tc := range bad {
		if err := ValidateIssue(tc.media, tc.kind, tc.desc, tc.part); err != tc.want {
			t.Errorf("%s: err = %v, want %v", name, err, tc.want)
		}
	}
}

func TestSelectionKeyIgnoresOrderAndTrackTitles(t *testing.T) {
	a := Selection{Seasons: []int{3, 1}, Episodes: []EpisodeRef{{Season: 5, Episode: 2}, {Season: 4, Episode: 9}}}
	b := Selection{Seasons: []int{1, 3}, Episodes: []EpisodeRef{{Season: 4, Episode: 9}, {Season: 5, Episode: 2}}}
	if a.Key() != b.Key() || a.Key() == "" {
		t.Fatalf("keys %q %q", a.Key(), b.Key())
	}
	t1 := Selection{Tracks: []TrackRef{{ID: "x", Title: "Old name"}, {ID: "y"}}}
	t2 := Selection{Tracks: []TrackRef{{ID: "y"}, {ID: "x", Title: "Renamed"}}}
	if t1.Key() != t2.Key() {
		t.Fatal("a retitled track is still the same track")
	}
	if (Selection{}).Key() != "" {
		t.Fatal("the whole item is the empty key")
	}
	if (Selection{Seasons: []int{1}}).Key() == (Selection{Seasons: []int{2}}).Key() {
		t.Fatal("different parts must differ")
	}
}

func TestIssueTransitions(t *testing.T) {
	cases := []struct {
		from, to IssueStatus
		ok       bool
	}{
		{IssueOpen, IssueResolved, true}, {IssueOpen, IssueClosed, true},
		{IssueResolved, IssueOpen, true}, {IssueClosed, IssueOpen, true},
		{IssueOpen, IssueInProgress, true}, {IssueInProgress, IssueResolved, true}, {IssueInProgress, IssueOpen, true}, {IssueResolved, IssueClosed, true},
		{IssueOpen, IssueOpen, false}, {IssueInProgress, IssueInProgress, false}, {IssueOpen, "nonsense", false}, {"nonsense", IssueOpen, false},
	}
	for _, tc := range cases {
		if got := CanTransitionIssue(tc.from, tc.to); got != tc.ok {
			t.Errorf("%s -> %s = %v", tc.from, tc.to, got)
		}
	}
}
