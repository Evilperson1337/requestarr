// Package setup holds the connection-check vocabulary shared by the library
// connection service and the checker: the targets that can be probed, their
// result codes, and the non-secret runtime facts a probe starts from.
//
// The seven-step setup draft wizard that used to live here was retired in D038;
// what remains is the input the checker still takes.
package setup

import (
	"context"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/evilperson1337/requestarr/internal/config"
	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/secrets"
	"github.com/evilperson1337/requestarr/internal/sources"
)

// Checker probes one target with the given settings. Describe is the message
// shown for a result, including which secret source was verified.
type Checker interface {
	Check(context.Context, Target, Settings) (CheckCode, error)
	Describe(context.Context, Target, CheckCode) string
}

// SecretSources resolves the value the next restart would use for a secret and
// says where it comes from (D024). The value must never leave the server.
type SecretSources interface {
	Resolve(context.Context, secrets.Name) (secrets.Resolved, error)
}

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

// The fixed environment names a check may reference, read from the secret and
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

// Settings is the closed, non-secret input of a connection check, never an arbitrary configuration map.
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
	// PrivateHosts is the configuration package's rule for which library hosts
	// may use http outside development (D033).
	PrivateHosts config.PrivateHosts
}

// SecretConfigured reports whether a value for the secret is in effect.
func (r Runtime) SecretConfigured(name secrets.Name) bool { return r.SecretsConfigured[name] }

// LibraryUsername is the plain username in effect for a Source slug.
func (r Runtime) LibraryUsername(slug string) string { return r.LibraryUsernames[slug] }

func ValidServiceURL(raw string, development bool) bool {
	if len(raw) > 2048 || !utf8.ValidString(raw) || strings.ContainsAny(raw, "\r\n\x00") {
		return false
	}
	u, err := url.Parse(raw)
	return err == nil && u.IsAbs() && u.Hostname() != "" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && u.Opaque == "" && (u.Scheme == "https" || development && u.Scheme == "http")
}

// ValidSourceURL restricts library endpoints to an origin; credentials are sent only to that origin.
// Outside development, http is allowed only to a private host, by the same
// rule the environment configuration uses.
func ValidSourceURL(raw string, rt Runtime) bool {
	if !ValidServiceURL(raw, true) {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Path != "" && u.Path != "/") {
		return false
	}
	return u.Scheme == "https" || rt.Development || rt.PrivateHosts.Contains(u.Hostname())
}
