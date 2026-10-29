// Package sources is the registry of Source kinds: Jellyfin, Plex, RomM and
// Navidrome. A kind fixes a Source's slug, stable ID, media types and the
// Credential fields it needs, so the setup wizard, configuration, startup
// wiring and the web UI all read one table instead of switching on a string.
//
// It is plain data: it imports only domain and no HTTP, database, provider or
// configuration code. Provider clients stay in internal/inventory.
package sources

import "github.com/evilperson1337/requestarr/internal/domain"

// Stable Source IDs. V1 allows one configured Source per kind.
const (
	JellyfinID  = "5efcc577-5236-4ce3-b2c0-b9d58c64a718"
	PlexID      = "8d1b6e34-5a3f-4c0e-9a57-2f0f6a9c1d42"
	RommID      = "c2f7b0a4-6d19-4e55-8b3e-5a1f9d7e3c68"
	NavidromeID = "b44ce1fc-fbcb-4d36-8618-52de83e84a6d"
)

// Role is what a Credential field is for.
type Role string

const (
	RoleToken    Role = "token"
	RoleUsername Role = "username"
	RolePassword Role = "password"
)

// Credential is one input a Source kind needs besides its URL. A Secret field
// is write-only and may be a Stored secret; a plain field (a username) is not.
type Credential struct {
	Role Role
	// Env is the environment variable that supplies the field.
	Env string
	// Secret names the stored secret behind the field; empty for a plain field.
	Secret string
	// Noun is what the field is called in check messages ("token", "password").
	Noun string
}

// Kind describes one Source kind.
type Kind struct {
	Slug       string
	Name       string
	ID         string
	URLEnv     string
	MediaTypes []domain.MediaType
	// Credentials is ordered; every field must be configured with the URL.
	Credentials []Credential
}

// Credential returns the field with the given role.
func (k Kind) Credential(role Role) (Credential, bool) {
	for _, c := range k.Credentials {
		if c.Role == role {
			return c, true
		}
	}
	return Credential{}, false
}

// Covers reports whether the kind may declare the media type.
func (k Kind) Covers(media domain.MediaType) bool {
	for _, m := range k.MediaTypes {
		if m == media {
			return true
		}
	}
	return false
}

// DefaultMediaTypes returns a copy of the media types the kind covers by default.
func (k Kind) DefaultMediaTypes() []domain.MediaType {
	return append([]domain.MediaType{}, k.MediaTypes...)
}

var kinds = []Kind{
	{Slug: "jellyfin", Name: "Jellyfin", ID: JellyfinID, URLEnv: "REQUESTARR_JELLYFIN_URL",
		MediaTypes:  []domain.MediaType{domain.MediaMovie, domain.MediaTV},
		Credentials: []Credential{{Role: RoleToken, Env: "REQUESTARR_JELLYFIN_API_KEY", Secret: "jellyfin_api_key", Noun: "API key"}}},
	{Slug: "plex", Name: "Plex", ID: PlexID, URLEnv: "REQUESTARR_PLEX_URL",
		MediaTypes:  []domain.MediaType{domain.MediaMovie, domain.MediaTV, domain.MediaAlbum, domain.MediaSong},
		Credentials: []Credential{{Role: RoleToken, Env: "REQUESTARR_PLEX_TOKEN", Secret: "plex_token", Noun: "token"}}},
	{Slug: "romm", Name: "RomM", ID: RommID, URLEnv: "REQUESTARR_ROMM_URL",
		MediaTypes:  []domain.MediaType{domain.MediaGame},
		Credentials: []Credential{{Role: RoleToken, Env: "REQUESTARR_ROMM_TOKEN", Secret: "romm_token", Noun: "token"}}},
	{Slug: "navidrome", Name: "Navidrome", ID: NavidromeID, URLEnv: "REQUESTARR_NAVIDROME_URL",
		MediaTypes: []domain.MediaType{domain.MediaAlbum, domain.MediaSong},
		Credentials: []Credential{
			{Role: RoleUsername, Env: "REQUESTARR_NAVIDROME_USERNAME"},
			{Role: RolePassword, Env: "REQUESTARR_NAVIDROME_PASSWORD", Secret: "navidrome_password", Noun: "password"},
		}},
}

// All returns every Source kind in a stable order.
func All() []Kind {
	out := make([]Kind, len(kinds))
	for i, k := range kinds {
		out[i] = copyOf(k)
	}
	return out
}

// Lookup returns the kind for a slug.
func Lookup(slug string) (Kind, bool) {
	for _, k := range kinds {
		if k.Slug == slug {
			return copyOf(k), true
		}
	}
	return Kind{}, false
}

func copyOf(k Kind) Kind {
	k.MediaTypes = k.DefaultMediaTypes()
	k.Credentials = append([]Credential(nil), k.Credentials...)
	return k
}

// MustLookup is Lookup for a slug known at compile time.
func MustLookup(slug string) Kind {
	k, ok := Lookup(slug)
	if !ok {
		panic("unknown Source kind " + slug)
	}
	return k
}
