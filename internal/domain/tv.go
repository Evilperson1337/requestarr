package domain

// TVDetails describes a whole series. Counts are informational and never
// introduce season/episode request selection or change canonical identity.
type TVDetails struct {
	Overview           string      `json:"overview,omitempty"`
	EpisodeRuntime     int         `json:"episodeRuntime,omitempty"`
	Genres             []string    `json:"genres,omitempty"`
	FirstAirDate       string      `json:"firstAirDate,omitempty"`
	LastAirDate        string      `json:"lastAirDate,omitempty"`
	SeriesStatus       string      `json:"seriesStatus,omitempty"`
	Seasons            int         `json:"seasons,omitempty"`
	Episodes           int         `json:"episodes,omitempty"`
	Score              *float64    `json:"score,omitempty"`
	RatingCount        int64       `json:"ratingCount,omitempty"`
	BackdropURL        string      `json:"backdropUrl,omitempty"`
	Creators           []string    `json:"creators,omitempty"`
	Cast               []MovieCast `json:"cast,omitempty"`
	DetailsUnavailable bool        `json:"detailsUnavailable,omitempty"`
}
