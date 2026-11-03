package tmdb

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/providers/metadata"
)

const (
	maxSeasons = 50
	// TMDB allows 20 appended responses to one request.
	maxAppended = 20
	maxEpisodes = 500
)

// episodes fills each listed season's episodes with one appended request. It
// is optional enrichment on its own short budget: when it fails the seasons
// stay requestable whole and nothing is marked unavailable.
func (r *Resolver) episodes(ctx context.Context, endpoint string, headers http.Header, tv *domain.TVDetails) {
	var keys []string
	for _, season := range tv.SeasonList {
		if len(keys) < maxAppended {
			keys = append(keys, fmt.Sprintf("season/%d", season.Number))
		}
	}
	if len(keys) == 0 {
		return
	}
	budget := 2 * time.Second
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline) - 200*time.Millisecond; remaining < budget {
			budget = remaining
		}
	}
	if budget <= 0 {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	var appended map[string]json.RawMessage
	if err := metadata.Fetch(ctx, r.client, endpoint+"?append_to_response="+strings.Join(keys, ","), headers, &appended); err != nil {
		return
	}
	for i := range tv.SeasonList {
		raw, ok := appended[fmt.Sprintf("season/%d", tv.SeasonList[i].Number)]
		if !ok {
			continue
		}
		var season struct {
			Episodes []struct {
				Number  int    `json:"episode_number"`
				Name    string `json:"name"`
				AirDate string `json:"air_date"`
			} `json:"episodes"`
		}
		if json.Unmarshal(raw, &season) != nil {
			continue
		}
		for _, e := range season.Episodes {
			if e.Number < 1 || e.Number > 100000 || len(tv.SeasonList[i].Episodes) >= maxEpisodes {
				continue
			}
			episode := domain.TVEpisode{Number: e.Number, Name: cleanText(e.Name, 256)}
			if _, err := time.Parse("2006-01-02", e.AirDate); err == nil {
				episode.AirDate = e.AirDate
			}
			tv.SeasonList[i].Episodes = append(tv.SeasonList[i].Episodes, episode)
		}
	}
}
