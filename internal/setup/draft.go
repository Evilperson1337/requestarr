package setup

import (
	"errors"
	"net/mail"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrForbidden = errors.New("administrator role required")
	ErrInvalid   = errors.New("invalid setup draft")
	ErrConflict  = errors.New("setup draft revision conflict")
)

const (
	OIDCSecretEnv = "REQUESTARR_OIDC_CLIENT_SECRET"
	TMDBTokenEnv  = "REQUESTARR_TMDB_READ_TOKEN"
	IGDBSecretEnv = "REQUESTARR_IGDB_CLIENT_SECRET"
)

type Actor struct{ ID, Role string }

// Settings is a closed non-secret schema, never an arbitrary configuration map.
// Environment references are allowlisted, not arbitrary environment lookups.
type Settings struct {
	General        GeneralSettings        `json:"general"`
	Authentication AuthenticationSettings `json:"authentication"`
	Providers      ProviderSettings       `json:"providers"`
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
)

func (t Target) Valid() bool {
	return t == TargetGeneral || t == TargetAuthentication || t == TargetTMDB || t == TargetMusicBrainz || t == TargetIGDB
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
	return nil
}

func ValidServiceURL(raw string, development bool) bool {
	if len(raw) > 2048 || !utf8.ValidString(raw) || strings.ContainsAny(raw, "\r\n\x00") {
		return false
	}
	u, err := url.Parse(raw)
	return err == nil && u.IsAbs() && u.Hostname() != "" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && u.Opaque == "" && (u.Scheme == "https" || development && u.Scheme == "http")
}
