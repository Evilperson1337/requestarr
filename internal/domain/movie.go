package domain

// MovieDetails is the bounded, provider-independent preview read model. Missing
// facts are absent, not invented zero values. Provider payloads never escape it.
type MovieDetails struct {
	Overview           string      `json:"overview,omitempty"`
	Runtime            int         `json:"runtime,omitempty"`
	Genres             []string    `json:"genres,omitempty"`
	ReleaseDate        string      `json:"releaseDate,omitempty"`
	ReleaseStatus      string      `json:"releaseStatus,omitempty"`
	Score              *float64    `json:"score,omitempty"`
	RatingCount        int64       `json:"ratingCount,omitempty"`
	BackdropURL        string      `json:"backdropUrl,omitempty"`
	Directors          []string    `json:"directors,omitempty"`
	Cast               []MovieCast `json:"cast,omitempty"`
	DetailsUnavailable bool        `json:"detailsUnavailable,omitempty"`
}

type MovieCast struct {
	Name      string `json:"name"`
	Character string `json:"character,omitempty"`
}
