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

var issueKindLabels = map[IssueKind]string{
	IssueVideo: "Video", IssueAudio: "Audio", IssueSubtitles: "Subtitles", IssueWrongItem: "Wrong item",
	IssueMissingTracks: "Missing or wrong tracks", IssueWrongTags: "Wrong tags or artwork",
	IssueNoBoot: "Doesn't boot or launch", IssueWrongRegion: "Wrong region", IssueWrongVersion: "Wrong version or platform",
	IssueMissingFiles: "Missing files", IssueOther: "Other",
}

// Valid reports whether the kind exists at all (for any media type).
func (k IssueKind) Valid() bool { _, ok := issueKindLabels[k]; return ok }

// Label is the kind as people read it.
func (k IssueKind) Label() string {
	if label, ok := issueKindLabels[k]; ok {
		return label
	}
	return string(k)
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

// IssueEvent is one thing that happened to an Issue, oldest first in a timeline.
type IssueEvent struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	ActorName string    `json:"actorName"`
	Comment   string    `json:"comment,omitempty"`
	At        time.Time `json:"at"`
}

// IssueReporter is someone who reported or joined an Issue, with their own note.
type IssueReporter struct {
	UserID string    `json:"userId"`
	Name   string    `json:"name"`
	Note   string    `json:"note,omitempty"`
	At     time.Time `json:"at"`
	Left   bool      `json:"left"`
}

// IssueQuery filters the Issues list.
type IssueQuery struct {
	Status    IssueStatus
	Kind      IssueKind
	MediaType MediaType
	Limit     int
	After     string
}

type IssuePage struct {
	Items      []Issue
	NextCursor *string
}

// IssueDetail is an Issue with its timeline. Reporters lists everyone for an
// admin and only the viewer for anyone else.
type IssueDetail struct {
	Issue       Issue
	Events      []IssueEvent
	Reporters   []IssueReporter
	YouReported bool
}
