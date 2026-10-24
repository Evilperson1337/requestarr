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
)

type Actor struct{ ID, Role string }

// Settings is a closed non-secret schema, never an arbitrary configuration map.
// Environment references are allowlisted, not arbitrary environment lookups.
type Settings struct {
	General        GeneralSettings        `json:"general"`
	Authentication AuthenticationSettings `json:"authentication"`
	Providers      ProviderSettings       `json:"providers"`
	Libraries      LibrariesSettings      `json:"libraries"`
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
)

func (t Target) Valid() bool {
	return t == TargetGeneral || t == TargetAuthentication || t == TargetTMDB || t == TargetMusicBrainz || t == TargetIGDB || t == TargetJellyfin || t == TargetPlex || t == TargetRomm || t == TargetNavidrome
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
