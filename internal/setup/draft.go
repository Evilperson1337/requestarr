package setup

import (
	"errors"
	"net/mail"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/evilperson1337/requestarr/internal/domain"
)

var (
	ErrForbidden = errors.New("administrator role required")
	ErrInvalid   = errors.New("invalid setup draft")
	ErrConflict  = errors.New("setup draft revision conflict")
	// ErrCompleted is returned for edits after Finish; the wizard no longer accepts changes.
	ErrCompleted = errors.New("setup already completed")
	// ErrNotReady is returned by Finish while required checks are missing, failed or expired.
	ErrNotReady = errors.New("setup checks are not passing")
)

const (
	OIDCSecretEnv        = "REQUESTARR_OIDC_CLIENT_SECRET"
	TMDBTokenEnv         = "REQUESTARR_TMDB_READ_TOKEN"
	IGDBSecretEnv        = "REQUESTARR_IGDB_CLIENT_SECRET"
	JellyfinAPIKeyEnv    = "REQUESTARR_JELLYFIN_API_KEY"
	PlexTokenEnv         = "REQUESTARR_PLEX_TOKEN"
	RommTokenEnv         = "REQUESTARR_ROMM_TOKEN"
	NavidromeUsernameEnv = "REQUESTARR_NAVIDROME_USERNAME"
	NavidromePasswordEnv = "REQUESTARR_NAVIDROME_PASSWORD"
	AppriseConfigKeyEnv  = "REQUESTARR_APPRISE_CONFIG_KEY"
)

type Actor struct{ ID, Role string }

// Settings is a closed non-secret schema, never an arbitrary configuration map.
// Environment references are allowlisted, not arbitrary environment lookups.
type Settings struct {
	General        GeneralSettings        `json:"general"`
	Authentication AuthenticationSettings `json:"authentication"`
	Providers      ProviderSettings       `json:"providers"`
	Libraries      LibrariesSettings      `json:"libraries"`
	Notifications  NotificationSettings   `json:"notifications"`
	Policy         PolicySettings         `json:"policy"`
}

// NotificationSettings drafts the global Apprise route. Authentication stays
// OIDC-only; there is deliberately no credential field here.
type NotificationSettings struct {
	AppriseEnabled      bool   `json:"appriseEnabled"`
	AppriseURL          string `json:"appriseUrl"`
	AppriseConfigKeyEnv string `json:"appriseConfigKeyEnv"`
}

// PolicySettings drafts request policy. Media types listed here skip
// administrator approval; an empty list means every request awaits approval.
type PolicySettings struct {
	AutoApproveMediaTypes []domain.MediaType `json:"autoApproveMediaTypes"`
}

type LibrariesSettings struct {
	Jellyfin  SourceSettings `json:"jellyfin"`
	Plex      SourceSettings `json:"plex"`
	Romm      SourceSettings `json:"romm"`
	Navidrome SourceSettings `json:"navidrome"`
}

// SourceSettings contains only safe draft values and fixed environment names.
type SourceSettings struct {
	Enabled     bool               `json:"enabled"`
	URL         string             `json:"url"`
	MediaTypes  []domain.MediaType `json:"mediaTypes"`
	TokenEnv    string             `json:"tokenEnv"`
	UsernameEnv string             `json:"usernameEnv"`
	PasswordEnv string             `json:"passwordEnv"`
}

type GeneralSettings struct {
	InstanceName string `json:"instanceName"`
	Timezone     string `json:"timezone"`
	PublicURL    string `json:"publicUrl"`
}

type AuthenticationSettings struct {
	Issuer          string `json:"issuer"`
	ClientID        string `json:"clientId"`
	ClientSecretEnv string `json:"clientSecretEnv"`
}

type ProviderSettings struct {
	TMDBEnabled         bool   `json:"tmdbEnabled"`
	TMDBTokenEnv        string `json:"tmdbTokenEnv"`
	MusicBrainzEnabled  bool   `json:"musicBrainzEnabled"`
	MusicBrainzContact  string `json:"musicBrainzContact"`
	IGDBEnabled         bool   `json:"igdbEnabled"`
	IGDBClientID        string `json:"igdbClientId"`
	IGDBClientSecretEnv string `json:"igdbClientSecretEnv"`
}

type Target string

const (
	TargetGeneral        Target = "general"
	TargetAuthentication Target = "authentication"
	TargetTMDB           Target = "tmdb"
	TargetMusicBrainz    Target = "musicbrainz"
	TargetIGDB           Target = "igdb"
	TargetJellyfin       Target = "library:jellyfin"
	TargetPlex           Target = "library:plex"
	TargetRomm           Target = "library:romm"
	TargetNavidrome      Target = "library:navidrome"
	TargetNotifications  Target = "notifications"
)

func (t Target) Valid() bool {
	return t == TargetGeneral || t == TargetAuthentication || t == TargetTMDB || t == TargetMusicBrainz || t == TargetIGDB || t == TargetJellyfin || t == TargetPlex || t == TargetRomm || t == TargetNavidrome || t == TargetNotifications
}

func LibrarySource(t Target) (string, bool) {
	switch t {
	case TargetJellyfin:
		return "jellyfin", true
	case TargetPlex:
		return "plex", true
	case TargetRomm:
		return "romm", true
	case TargetNavidrome:
		return "navidrome", true
	default:
		return "", false
	}
}

type CheckCode string

const (
	CheckOK          CheckCode = "connected"
	CheckUnavailable CheckCode = "connection_failed"
	CheckEnvironment CheckCode = "environment_required"
	CheckIncomplete  CheckCode = "incomplete_settings"
	CheckDisabled    CheckCode = "disabled"
)

type Validation struct {
	Target    Target    `json:"target"`
	Revision  int64     `json:"revision"`
	Valid     bool      `json:"valid"`
	Code      CheckCode `json:"code"`
	Message   string    `json:"message"`
	CheckedAt time.Time `json:"checkedAt"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type Draft struct {
	Revision    int64             `json:"revision"`
	CurrentStep Step              `json:"currentStep"`
	Settings    Settings          `json:"settings"`
	Validations []Validation      `json:"validations"`
	UpdatedAt   time.Time         `json:"updatedAt"`
	Inactive    bool              `json:"inactive"`
	Environment []EnvironmentItem `json:"environment"`
	// Blockers lists required checks that are missing, failed or expired for this revision.
	Blockers    []Blocker  `json:"blockers"`
	CompletedAt *time.Time `json:"completedAt"`
}

type Blocker struct {
	Target Target `json:"target"`
	Reason string `json:"reason"`
}

type EnvironmentItem struct {
	Name         string `json:"name"`
	Secret       bool   `json:"secret"`
	Configured   bool   `json:"configured"`
	NeedsRestart bool   `json:"needsRestart"`
	DesiredValue string `json:"desiredValue,omitempty"`
}

// Runtime carries safe configuration and secret-presence flags, not secrets.
type Runtime struct {
	PublicURL, OIDCIssuer, OIDCClientID, IGDBClientID               string
	OIDCSecretConfigured, TMDBTokenConfigured, IGDBSecretConfigured bool
	LibrarySecrets                                                  map[string]bool
	LibraryURLs                                                     map[string]string
	AppriseURL                                                      string
	AppriseKeyConfigured                                            bool
	Development                                                     bool
}

type SaveInput struct {
	ExpectedRevision int64    `json:"expectedRevision"`
	CurrentStep      Step     `json:"currentStep"`
	Settings         Settings `json:"settings"`
}

func (s Settings) Validate(development bool) error {
	for _, field := range []struct {
		value string
		limit int
	}{
		{s.General.InstanceName, 120}, {s.General.Timezone, 128}, {s.Authentication.ClientID, 256}, {s.Providers.IGDBClientID, 256}, {s.Providers.MusicBrainzContact, 254},
	} {
		if !utf8.ValidString(field.value) || utf8.RuneCountInString(field.value) > field.limit || strings.ContainsAny(field.value, "\x00\r\n") {
			return ErrInvalid
		}
	}
	if s.General.Timezone != "" {
		if _, err := time.LoadLocation(s.General.Timezone); err != nil {
			return ErrInvalid
		}
	}
	for _, raw := range []string{s.General.PublicURL, s.Authentication.Issuer} {
		if raw != "" && !ValidServiceURL(raw, development) {
			return ErrInvalid
		}
	}
	if s.Authentication.ClientSecretEnv != OIDCSecretEnv || s.Providers.TMDBTokenEnv != TMDBTokenEnv || s.Providers.IGDBClientSecretEnv != IGDBSecretEnv {
		return ErrInvalid
	}
	if s.Providers.MusicBrainzContact != "" {
		address, err := mail.ParseAddress(s.Providers.MusicBrainzContact)
		if err != nil || address.Address != s.Providers.MusicBrainzContact {
			return ErrInvalid
		}
	}
	if n := s.Notifications; n.AppriseConfigKeyEnv != AppriseConfigKeyEnv || n.AppriseURL != "" && !ValidServiceURL(n.AppriseURL, development) {
		return ErrInvalid
	}
	seenAuto := map[domain.MediaType]bool{}
	for _, media := range s.Policy.AutoApproveMediaTypes {
		if !validMedia(media) || seenAuto[media] {
			return ErrInvalid
		}
		seenAuto[media] = true
	}
	for kind, source := range map[string]SourceSettings{"jellyfin": s.Libraries.Jellyfin, "plex": s.Libraries.Plex, "romm": s.Libraries.Romm, "navidrome": s.Libraries.Navidrome} {
		if source.URL != "" && !ValidSourceURL(source.URL, development) {
			return ErrInvalid
		}
		if !validMediaTypes(kind, source.MediaTypes) {
			return ErrInvalid
		}
		wantToken := map[string]string{"jellyfin": JellyfinAPIKeyEnv, "plex": PlexTokenEnv, "romm": RommTokenEnv}[kind]
		if kind == "navidrome" {
			if source.TokenEnv != "" || source.UsernameEnv != NavidromeUsernameEnv || source.PasswordEnv != NavidromePasswordEnv {
				return ErrInvalid
			}
		} else if source.TokenEnv != wantToken || source.UsernameEnv != "" || source.PasswordEnv != "" {
			return ErrInvalid
		}
	}
	return nil
}

func validMediaTypes(kind string, types []domain.MediaType) bool {
	allowed := map[string]map[domain.MediaType]bool{
		"jellyfin":  {domain.MediaMovie: true, domain.MediaTV: true},
		"plex":      {domain.MediaMovie: true, domain.MediaTV: true, domain.MediaAlbum: true, domain.MediaSong: true},
		"romm":      {domain.MediaGame: true},
		"navidrome": {domain.MediaAlbum: true, domain.MediaSong: true},
	}[kind]
	seen := make(map[domain.MediaType]bool, len(types))
	for _, media := range types {
		if !allowed[media] || seen[media] {
			return false
		}
		seen[media] = true
	}
	return true
}

func ValidServiceURL(raw string, development bool) bool {
	if len(raw) > 2048 || !utf8.ValidString(raw) || strings.ContainsAny(raw, "\r\n\x00") {
		return false
	}
	u, err := url.Parse(raw)
	return err == nil && u.IsAbs() && u.Hostname() != "" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && u.Opaque == "" && (u.Scheme == "https" || development && u.Scheme == "http")
}

// ValidSourceURL restricts library endpoints to an origin; credentials are sent only to that origin.
func ValidSourceURL(raw string, development bool) bool {
	if !ValidServiceURL(raw, development) {
		return false
	}
	u, err := url.Parse(raw)
	return err == nil && (u.Path == "" || u.Path == "/")
}

// RequiredTargets lists every check that must pass before setup can finish.
func (s Settings) RequiredTargets() []Target {
	targets := []Target{TargetGeneral, TargetAuthentication}
	if s.Providers.TMDBEnabled {
		targets = append(targets, TargetTMDB)
	}
	if s.Providers.MusicBrainzEnabled {
		targets = append(targets, TargetMusicBrainz)
	}
	if s.Providers.IGDBEnabled {
		targets = append(targets, TargetIGDB)
	}
	for _, entry := range []struct {
		target  Target
		enabled bool
	}{
		{TargetJellyfin, s.Libraries.Jellyfin.Enabled}, {TargetPlex, s.Libraries.Plex.Enabled},
		{TargetRomm, s.Libraries.Romm.Enabled}, {TargetNavidrome, s.Libraries.Navidrome.Enabled},
		{TargetNotifications, s.Notifications.AppriseEnabled},
	} {
		if entry.enabled {
			targets = append(targets, entry.target)
		}
	}
	return targets
}

// pendingBlockers reports required targets without a passing, unexpired check for the draft revision.
func (d Draft) pendingBlockers(now time.Time) []Blocker {
	blockers := []Blocker{}
	for _, target := range d.Settings.RequiredTargets() {
		reason := "not_checked"
		for _, v := range d.Validations {
			if v.Target != target || v.Revision != d.Revision {
				continue
			}
			switch {
			case !v.Valid:
				reason = "failed"
			case !v.ExpiresAt.After(now):
				reason = "expired"
			default:
				reason = ""
			}
		}
		if reason != "" {
			blockers = append(blockers, Blocker{Target: target, Reason: reason})
		}
	}
	return blockers
}

func validMedia(media domain.MediaType) bool {
	switch media {
	case domain.MediaMovie, domain.MediaTV, domain.MediaAlbum, domain.MediaSong, domain.MediaGame:
		return true
	}
	return false
}
