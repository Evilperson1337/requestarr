package domain

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type IssueStatus string

const (
	IssueOpen     IssueStatus = "open"
	IssueResolved IssueStatus = "resolved"
	// IssueClosed is "won't fix": an admin declined it without pretending it was fixed.
	IssueClosed IssueStatus = "closed"
)

// IssueKind is the category of an Issue. The kinds offered depend on the media type.
type IssueKind string

const (
	IssueVideo         IssueKind = "video"
	IssueAudio         IssueKind = "audio"
	IssueSubtitles     IssueKind = "subtitles"
	IssueWrongItem     IssueKind = "wrong_item"
	IssueMissingTracks IssueKind = "missing_tracks"
	IssueWrongTags     IssueKind = "wrong_tags"
	IssueNoBoot        IssueKind = "no_boot"
	IssueWrongRegion   IssueKind = "wrong_region"
	IssueWrongVersion  IssueKind = "wrong_version"
	IssueMissingFiles  IssueKind = "missing_files"
	IssueOther         IssueKind = "other"
)

const MaxIssueDescription = 2000

// IssueKindsFor lists the kinds offered for a media type, "other" always last.
func IssueKindsFor(media MediaType) []IssueKind {
	switch media {
	case MediaMovie, MediaTV:
		return []IssueKind{IssueVideo, IssueAudio, IssueSubtitles, IssueWrongItem, IssueOther}
	case MediaAlbum, MediaSong:
		return []IssueKind{IssueAudio, IssueMissingTracks, IssueWrongTags, IssueOther}
	case MediaGame:
		return []IssueKind{IssueNoBoot, IssueWrongRegion, IssueWrongVersion, IssueMissingFiles, IssueOther}
	}
	return nil
}

// Issue is a report that a Library item has a problem. It is about something a
// Source already holds, so it is never a Request and never causes a download.
type Issue struct {
	ID            string
	LibraryItemID string
	Title         string
	MediaType     MediaType
	SourceName    string
	Kind          IssueKind
	// Part is which season, episode or track is affected; empty means the whole item.
	Part        Selection
	Status      IssueStatus
	ReportedBy  string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	ResolvedAt  *time.Time
	Reporters   int
	Description string
	// GoneFromLibrary is true when the item's Source has since synced without it.
	GoneFromLibrary bool
	Version         int64
}

var (
	ErrIssueKind        = errors.New("that kind is not offered for this item")
	ErrIssueDescription = errors.New("a description is required for Other, and may be at most 2,000 characters")
	ErrIssuePart        = errors.New("that part cannot be reported for this item")
)

// ValidateIssue checks a new report against the item's media type.
func ValidateIssue(media MediaType, kind IssueKind, description string, part Selection) error {
	if !slices.Contains(IssueKindsFor(media), kind) {
		return ErrIssueKind
	}
	description = strings.TrimSpace(description)
	if utf8.RuneCountInString(description) > MaxIssueDescription || !utf8.ValidString(description) || strings.IndexFunc(description, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\t' }) >= 0 {
		return ErrIssueDescription
	}
	if kind == IssueOther && description == "" {
		return ErrIssueDescription
	}
	if err := part.Validate(media); err != nil {
		return ErrIssuePart
	}
	return nil
}

// Key is a canonical string for a Selection, so two reports of the same part
// merge however the client ordered it. The whole item is the empty string.
func (s Selection) Key() string {
	if s.IsAll() {
		return ""
	}
	norm := Selection{}
	norm.Seasons = slices.Sorted(slices.Values(s.Seasons))
	norm.Episodes = slices.Clone(s.Episodes)
	slices.SortFunc(norm.Episodes, func(a, b EpisodeRef) int {
		if a.Season != b.Season {
			return a.Season - b.Season
		}
		return a.Episode - b.Episode
	})
	for _, t := range s.Tracks {
		norm.Tracks = append(norm.Tracks, TrackRef{ID: t.ID})
	}
	slices.SortFunc(norm.Tracks, func(a, b TrackRef) int { return strings.Compare(a.ID, b.ID) })
	out, _ := json.Marshal(norm)
	return string(out)
}

// CanTransitionIssue encodes who may move an Issue and where: only open Issues
// can be resolved or closed, and only a decided one can be reopened.
func CanTransitionIssue(from, to IssueStatus) bool {
	switch to {
	case IssueResolved, IssueClosed:
		return from == IssueOpen
	case IssueOpen:
		return from == IssueResolved || from == IssueClosed
	}
	return false
}
