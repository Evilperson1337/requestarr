package domain

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

// Selection is the part of an item a Request covers. The zero value is the
// whole item, so every Request made before selections existed still reads
// correctly. An album can select tracks; a series can select whole seasons and
// single episodes of seasons that are not selected whole. Merging selections
// into one shared Request never narrows it (D027).
type Selection struct {
	Tracks   []TrackRef   `json:"tracks,omitempty"`
	Seasons  []int        `json:"seasons,omitempty"`
	Episodes []EpisodeRef `json:"episodes,omitempty"`
}

// TrackRef is one requested track, snapshotted so a Request still reads the
// same if the provider later renumbers or retitles the release.
type TrackRef struct {
	ID       string `json:"id"`
	Position int    `json:"position,omitempty"`
	Title    string `json:"title"`
}

type EpisodeRef struct {
	Season  int `json:"season"`
	Episode int `json:"episode"`
}

const (
	maxTracks   = 500
	maxSeasons  = 1000
	maxEpisodes = 5000
)

// IsAll reports whether the Selection is the whole item.
func (s Selection) IsAll() bool {
	return len(s.Tracks) == 0 && len(s.Seasons) == 0 && len(s.Episodes) == 0
}

// Merge is the union of two selections. The whole item absorbs any subset.
func (s Selection) Merge(o Selection) Selection {
	if s.IsAll() || o.IsAll() {
		return Selection{}
	}
	out := Selection{}
	seenTrack := map[string]bool{}
	for _, t := range append(slices.Clone(s.Tracks), o.Tracks...) {
		if !seenTrack[t.ID] {
			seenTrack[t.ID] = true
			out.Tracks = append(out.Tracks, t)
		}
	}
	out.Seasons = slices.Compact(slices.Sorted(slices.Values(append(slices.Clone(s.Seasons), o.Seasons...))))
	if len(out.Seasons) == 0 {
		out.Seasons = nil
	}
	whole := map[int]bool{}
	for _, season := range out.Seasons {
		whole[season] = true
	}
	seenEpisode := map[EpisodeRef]bool{}
	for _, e := range append(slices.Clone(s.Episodes), o.Episodes...) {
		if !whole[e.Season] && !seenEpisode[e] {
			seenEpisode[e] = true
			out.Episodes = append(out.Episodes, e)
		}
	}
	slices.SortFunc(out.Episodes, func(a, b EpisodeRef) int {
		if a.Season != b.Season {
			return a.Season - b.Season
		}
		return a.Episode - b.Episode
	})
	return out
}

// Contains reports whether every part of o is already covered by s.
func (s Selection) Contains(o Selection) bool {
	if s.IsAll() {
		return true
	}
	if o.IsAll() {
		return false
	}
	for _, t := range o.Tracks {
		if !slices.ContainsFunc(s.Tracks, func(have TrackRef) bool { return have.ID == t.ID }) {
			return false
		}
	}
	for _, season := range o.Seasons {
		if !slices.Contains(s.Seasons, season) {
			return false
		}
	}
	for _, e := range o.Episodes {
		if !slices.Contains(s.Seasons, e.Season) && !slices.Contains(s.Episodes, e) {
			return false
		}
	}
	return true
}

// Equal compares two selections as sets of what they cover.
func (s Selection) Equal(o Selection) bool { return s.Contains(o) && o.Contains(s) }

// Validate checks the Selection against the item's media type and bounds.
func (s Selection) Validate(media MediaType) error {
	if s.IsAll() {
		return nil
	}
	switch media {
	case MediaAlbum:
		if len(s.Seasons) > 0 || len(s.Episodes) > 0 {
			return errors.New("an album selection holds tracks only")
		}
	case MediaTV:
		if len(s.Tracks) > 0 {
			return errors.New("a series selection holds seasons and episodes only")
		}
	default:
		return fmt.Errorf("a %s request has no selection", media)
	}
	if len(s.Tracks) > maxTracks || len(s.Seasons) > maxSeasons || len(s.Episodes) > maxEpisodes {
		return errors.New("selection is too large")
	}
	seenTrack := map[string]bool{}
	for _, t := range s.Tracks {
		title := strings.TrimSpace(t.Title)
		if t.ID == "" || len(t.ID) > 64 || title == "" || len(t.Title) > 512 || !utf8.ValidString(t.Title) || t.Position < 0 || t.Position > 10000 || seenTrack[t.ID] {
			return errors.New("invalid track selection")
		}
		seenTrack[t.ID] = true
	}
	seenSeason := map[int]bool{}
	for _, season := range s.Seasons {
		if season < 0 || season > 999 || seenSeason[season] {
			return errors.New("invalid season selection")
		}
		seenSeason[season] = true
	}
	seenEpisode := map[EpisodeRef]bool{}
	for _, e := range s.Episodes {
		if e.Season < 0 || e.Season > 999 || e.Episode < 1 || e.Episode > 100000 || seenEpisode[e] || seenSeason[e.Season] {
			return errors.New("invalid episode selection")
		}
		seenEpisode[e] = true
	}
	return nil
}
