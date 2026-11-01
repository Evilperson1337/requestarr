package domain

import "testing"

func TestSelectionEmptyMeansTheWholeItem(t *testing.T) {
	if !(Selection{}).IsAll() || (Selection{Seasons: []int{1}}).IsAll() || (Selection{Tracks: []TrackRef{{ID: "a"}}}).IsAll() {
		t.Fatal("only an empty Selection is the whole item")
	}
}

func TestSelectionMerge(t *testing.T) {
	tracks := func(ids ...string) Selection {
		s := Selection{}
		for i, id := range ids {
			s.Tracks = append(s.Tracks, TrackRef{ID: id, Position: i + 1, Title: id})
		}
		return s
	}
	cases := []struct {
		name string
		a, b Selection
		want Selection
	}{
		{"whole absorbs a subset", Selection{}, tracks("a"), Selection{}},
		{"subset then whole", tracks("a"), Selection{}, Selection{}},
		{"tracks union without duplicates", tracks("a", "b"), tracks("b", "c"), Selection{Tracks: []TrackRef{{ID: "a", Position: 1, Title: "a"}, {ID: "b", Position: 2, Title: "b"}, {ID: "c", Position: 2, Title: "c"}}}},
		{"seasons union sorted", Selection{Seasons: []int{3, 1}}, Selection{Seasons: []int{2, 1}}, Selection{Seasons: []int{1, 2, 3}}},
		{"episodes inside a selected season are absorbed", Selection{Seasons: []int{1}}, Selection{Episodes: []EpisodeRef{{Season: 1, Episode: 4}, {Season: 2, Episode: 1}}}, Selection{Seasons: []int{1}, Episodes: []EpisodeRef{{Season: 2, Episode: 1}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.a.Merge(tc.b)
			if !got.Equal(tc.want) {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestSelectionContains(t *testing.T) {
	base := Selection{Seasons: []int{1}, Episodes: []EpisodeRef{{Season: 2, Episode: 3}}}
	if !base.Contains(Selection{Seasons: []int{1}}) || !base.Contains(Selection{Episodes: []EpisodeRef{{Season: 1, Episode: 9}, {Season: 2, Episode: 3}}}) {
		t.Fatal("selected seasons and episodes are contained")
	}
	if base.Contains(Selection{Seasons: []int{2}}) || base.Contains(Selection{}) {
		t.Fatal("an unselected season, or the whole item, is not contained")
	}
	if !(Selection{}).Contains(Selection{Seasons: []int{5}}) {
		t.Fatal("the whole item contains any subset")
	}
}

func TestSelectionValidate(t *testing.T) {
	ok := []struct {
		m MediaType
		s Selection
	}{
		{MediaAlbum, Selection{Tracks: []TrackRef{{ID: "7e4e4742-3054-472d-8567-2b24603c9473", Position: 1, Title: "One"}}}},
		{MediaTV, Selection{Seasons: []int{0, 1}, Episodes: []EpisodeRef{{Season: 3, Episode: 2}}}},
		{MediaMovie, Selection{}}, {MediaGame, Selection{}},
	}
	for _, tc := range ok {
		if err := tc.s.Validate(tc.m); err != nil {
			t.Errorf("%s %+v: %v", tc.m, tc.s, err)
		}
	}
	bad := []struct {
		name string
		m    MediaType
		s    Selection
	}{
		{"tracks on a movie", MediaMovie, Selection{Tracks: []TrackRef{{ID: "a", Title: "x"}}}},
		{"seasons on an album", MediaAlbum, Selection{Seasons: []int{1}}},
		{"track without id", MediaAlbum, Selection{Tracks: []TrackRef{{Title: "x"}}}},
		{"duplicate track", MediaAlbum, Selection{Tracks: []TrackRef{{ID: "a", Title: "x"}, {ID: "a", Title: "x"}}}},
		{"negative season", MediaTV, Selection{Seasons: []int{-1}}},
		{"episode zero", MediaTV, Selection{Episodes: []EpisodeRef{{Season: 1, Episode: 0}}}},
		{"duplicate season", MediaTV, Selection{Seasons: []int{1, 1}}},
		{"too many tracks", MediaAlbum, Selection{Tracks: func() []TrackRef {
			out := make([]TrackRef, 501)
			for i := range out {
				out[i] = TrackRef{ID: string(rune('a'+i%26)) + string(rune('a'+i/26)), Title: "x"}
			}
			return out
		}()}},
	}
	for _, tc := range bad {
		if err := tc.s.Validate(tc.m); err == nil {
			t.Errorf("%s: want an error", tc.name)
		}
	}
}
