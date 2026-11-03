package setup

import (
	"errors"
	"net/mail"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/secrets"
	"github.com/evilperson1337/requestarr/internal/sources"
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

// OIDCSecretEnv is the environment-only OIDC client secret; it is not a stored secret.
const OIDCSecretEnv = "REQUESTARR_OIDC_CLIENT_SECRET"

// EnvOf is the environment variable that supplies a registered secret.
func EnvOf(name secrets.Name) string {
	spec, _ := secrets.Lookup(name)
	return spec.EnvVar
}

// credentialEnv is the environment variable of a Source kind's credential field.
func credentialEnv(slug string, role sources.Role) string {
	credential, _ := sources.MustLookup(slug).Credential(role)
	return credential.Env
}

// The fixed environment names a draft may reference, read from the secret and
// Source kind registries so each name is written once.
var (
	TMDBTokenEnv         = EnvOf(secrets.TMDBReadToken)
	IGDBSecretEnv        = EnvOf(secrets.IGDBClientSecret)
	AppriseConfigKeyEnv  = EnvOf(secrets.AppriseConfigKey)
	JellyfinAPIKeyEnv    = credentialEnv("jellyfin", sources.RoleToken)
	PlexTokenEnv         = credentialEnv("plex", sources.RoleToken)
	RommTokenEnv         = credentialEnv("romm", sources.RoleToken)
	AudiobookshelfKeyEnv = credentialEnv("audiobookshelf", sources.RoleToken)
	NavidromeUsernameEnv = credentialEnv("navidrome", sources.RoleUsername)
	NavidromePasswordEnv = credentialEnv("navidrome", sources.RolePassword)
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
	Jellyfin       SourceSettings `json:"jellyfin"`
	Plex           SourceSettings `json:"plex"`
	Romm           SourceSettings `json:"romm"`
	Navidrome      SourceSettings `json:"navidrome"`
	Audiobookshelf SourceSettings `json:"audiobookshelf"`
}

// Source returns the draft settings of one Source kind by slug; nil for an
// unknown slug. This is the one place the struct fields are named.
func (l *LibrariesSettings) Source(slug string) *SourceSettings {
	switch slug {
	case "jellyfin":
		return &l.Jellyfin
	case "plex":
		return &l.Plex
	case "romm":
		return &l.Romm
	case "navidrome":
		return &l.Navidrome
	case "audiobookshelf":
		return &l.Audiobookshelf
	}
	return nil
}

// SourceSettings contains only safe draft values and fixed environment names.
type SourceSettings struct {
	Enabled    bool               `json:"enabled"`
	URL        string             `json:"url"`
	MediaTypes []domain.MediaType `json:"mediaTypes"`
	TokenEnv   string             `json:"tokenEnv"`
	// Username is an ordinary non-secret draft field (Navidrome only). Drafts
	// saved before it existed carried a usernameEnv reference; that key is
	// ignored on load and the environment-configured username takes its place.
	Username    string `json:"username"`
	PasswordEnv string `json:"passwordEnv"`
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
	TargetAudiobookshelf Target = "library:audiobookshelf"
	TargetNotifications  Target = "notifications"
)

func (t Target) Valid() bool {
	if _, ok := LibrarySource(t); ok {
		return true
	}
	return t == TargetGeneral || t == TargetAuthentication || t == TargetTMDB || t == TargetMusicBrainz || t == TargetIGDB || t == TargetNotifications
}

// LibraryTarget is the check target for a Source kind.
func LibraryTarget(slug string) Target { return Target("library:" + slug) }

// LibrarySource returns the Source kind slug a library check target names.
func LibrarySource(t Target) (string, bool) {
	slug, ok := strings.CutPrefix(string(t), "library:")
	if !ok {
		return "", false
	}
	if _, known := sources.Lookup(slug); !known {
		return "", false
	}
	return slug, true
}

type CheckCode string

const (
	CheckOK             CheckCode = "connected"
	CheckUnavailable    CheckCode = "connection_failed"
	CheckAuthentication CheckCode = "authentication_failed"
	CheckEnvironment    CheckCode = "environment_required"
	CheckIncomplete     CheckCode = "incomplete_settings"
	CheckDisabled       CheckCode = "disabled"
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
	PublicURL, OIDCIssuer, OIDCClientID, IGDBClientID string
	// IGDBClientIDFromEnvironment distinguishes an operator override from a
	// saved client ID loaded at boot. Checks may use a newer saved ID otherwise.
	IGDBClientIDFromEnvironment bool
	OIDCSecretConfigured        bool
	// SecretsConfigured says which stored-capable secrets have a value in
	// effect at boot (environment, else stored).
	SecretsConfigured map[secrets.Name]bool
	// LibraryUsernames are the plain usernames in effect at boot, by Source slug.
	LibraryUsernames map[string]string
	LibraryURLs      map[string]string
	AppriseURL       string
	Development      bool
}

// SecretConfigured reports whether a value for the secret is in effect.
func (r Runtime) SecretConfigured(name secrets.Name) bool { return r.SecretsConfigured[name] }

// LibraryUsername is the plain username in effect for a Source slug.
func (r Runtime) LibraryUsername(slug string) string { return r.LibraryUsernames[slug] }

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
	for _, kind := range sources.All() {
		source := *s.Libraries.Source(kind.Slug)
		if source.URL != "" && !ValidSourceURL(source.URL, development) {
			return ErrInvalid
		}
		if !validMediaTypes(kind, source.MediaTypes) {
			return ErrInvalid
		}
		if !source.credentialsValid(kind) {
			return ErrInvalid
		}
	}
	return nil
}

func validMediaTypes(kind sources.Kind, types []domain.MediaType) bool {
	seen := make(map[domain.MediaType]bool, len(types))
	for _, media := range types {
		if !kind.Covers(media) || seen[media] {
			return false
		}
		seen[media] = true
	}
	return true
}

// credentialsValid reports whether the draft names exactly the credential
// environment variables the kind declares, and a plain username only where the
// kind has one.
func (s SourceSettings) credentialsValid(kind sources.Kind) bool {
	token, hasToken := kind.Credential(sources.RoleToken)
	password, hasPassword := kind.Credential(sources.RolePassword)
	_, hasUsername := kind.Credential(sources.RoleUsername)
	if hasToken != (s.TokenEnv != "") || hasToken && s.TokenEnv != token.Env {
		return false
	}
	if hasPassword != (s.PasswordEnv != "") || hasPassword && s.PasswordEnv != password.Env {
		return false
	}
	if !hasUsername {
		return s.Username == ""
	}
	return utf8.ValidString(s.Username) && utf8.RuneCountInString(s.Username) <= 256 && !strings.ContainsAny(s.Username, "\x00\r\n")
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
	for _, kind := range sources.All() {
		if s.Libraries.Source(kind.Slug).Enabled {
			targets = append(targets, LibraryTarget(kind.Slug))
		}
	}
	if s.Notifications.AppriseEnabled {
		targets = append(targets, TargetNotifications)
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
	case domain.MediaMovie, domain.MediaTV, domain.MediaAlbum, domain.MediaSong, domain.MediaGame, domain.MediaAudiobook:
		return true
	}
	return false
}
