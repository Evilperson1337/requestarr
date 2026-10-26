package secrets

// Name identifies one stored-capable secret. Later integrations add entries to
// the registry; nothing else in this package knows about a specific provider.
type Name string

const TMDBReadToken Name = "tmdb_read_token"

// Library Source credentials. Source URLs stay environment-only (D024).
const (
	JellyfinAPIKey Name = "jellyfin_api_key"
	PlexToken      Name = "plex_token"
	RommToken      Name = "romm_token"
)

// Spec describes a registered secret.
type Spec struct {
	Name   Name
	EnvVar string // environment variable that sets it; the environment always wins
	Label  string
}

var registry = []Spec{
	{Name: TMDBReadToken, EnvVar: "REQUESTARR_TMDB_READ_TOKEN", Label: "TMDB read token"},
	{Name: JellyfinAPIKey, EnvVar: "REQUESTARR_JELLYFIN_API_KEY", Label: "Jellyfin API key"},
	{Name: PlexToken, EnvVar: "REQUESTARR_PLEX_TOKEN", Label: "Plex token"},
	{Name: RommToken, EnvVar: "REQUESTARR_ROMM_TOKEN", Label: "RomM token"},
}

// All returns the registered secrets in a stable order.
func All() []Spec { return append([]Spec(nil), registry...) }

// Lookup returns the spec for name.
func Lookup(name Name) (Spec, bool) {
	for _, s := range registry {
		if s.Name == name {
			return s, true
		}
	}
	return Spec{}, false
}
