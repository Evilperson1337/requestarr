package domain

// MusicDetails is what MusicBrainz says about an album or a song, shown on the
// request preview. Every field is optional.
type MusicDetails struct {
	Artists        []string `json:"artists,omitempty"`
	PrimaryType    string   `json:"primaryType,omitempty"`
	SecondaryTypes []string `json:"secondaryTypes,omitempty"`
	ReleaseDate    string   `json:"releaseDate,omitempty"`
	Genres         []string `json:"genres,omitempty"`
	// LengthSeconds is a song's duration.
	LengthSeconds int `json:"lengthSeconds,omitempty"`
	// AppearsOn lists the releases a song appears on.
	AppearsOn []string `json:"appearsOn,omitempty"`
	// Score is the community rating on MusicBrainz's 0-5 scale.
	Score       *float64 `json:"score,omitempty"`
	RatingCount int64    `json:"ratingCount,omitempty"`
}
