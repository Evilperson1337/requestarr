package domain

// GameDetails is what a game's metadata provider says about it, shown on the
// request preview. Every field is optional; a provider fills what it has.
type GameDetails struct {
	Overview    string   `json:"overview,omitempty"`
	Genres      []string `json:"genres,omitempty"`
	ReleaseDate string   `json:"releaseDate,omitempty"`
	Developers  []string `json:"developers,omitempty"`
	// Score is the provider's rating on a 0-100 scale.
	Score       *float64 `json:"score,omitempty"`
	RatingCount int64    `json:"ratingCount,omitempty"`
	BackdropURL string   `json:"backdropUrl,omitempty"`
}
