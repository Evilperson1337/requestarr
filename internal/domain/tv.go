package domain

// TVDetails describes a whole series. Its season list is what a Request's
// selection (D027) chooses from; none of it changes canonical identity.
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
	SeasonList         []TVSeason  `json:"seasonList,omitempty"`
	DetailsUnavailable bool        `json:"detailsUnavailable,omitempty"`
}

// TVSeason is one season. Episodes is empty when TMDB's episode lists could
// not be read; the season can still be requested whole.
type TVSeason struct {
	Number       int         `json:"number"`
	Name         string      `json:"name,omitempty"`
	EpisodeCount int         `json:"episodeCount,omitempty"`
	AirDate      string      `json:"airDate,omitempty"`
	Episodes     []TVEpisode `json:"episodes,omitempty"`
}

type TVEpisode struct {
	Number  int    `json:"number"`
	Name    string `json:"name,omitempty"`
	AirDate string `json:"airDate,omitempty"`
}
