package musicbrainz

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/providers/metadata"
)

// SongSearcher finds songs by name. It is the Songs type's searcher, kept apart
// from Resolver.Search (albums) because a Searcher serves exactly one type.
func (r *Resolver) SongSearcher() metadata.Searcher { return songSearcher{r} }

type songSearcher struct{ r *Resolver }

// Search runs one recording search. MusicBrainz already returns a recording
// once, with every release it appears on nested inside, so a row is a recording
// and the "first release" is the earliest dated release of that list. It waits
// in the same spacer as album search and previews, through searchFetch.
func (s songSearcher) Search(ctx context.Context, q metadata.SearchQuery) (domain.SearchPage, error) {
	query := termsQuery(q.Text, "recording")
	if query == "" {
		return domain.SearchPage{Results: []domain.SearchResult{}}, nil
	}
	page := max(q.Page, 1)
	offset := (page - 1) * searchPageSize
	params := url.Values{"query": {query}, "fmt": {"json"}, "limit": {strconv.Itoa(searchPageSize)}, "offset": {strconv.Itoa(offset)}}
	var data struct {
		Count      int `json:"count"`
		Recordings []struct {
			ID           string   `json:"id"`
			Title        string   `json:"title"`
			Length       int64    `json:"length"`
			FirstRelease string   `json:"first-release-date"`
			ArtistCredit []credit `json:"artist-credit"`
			Releases     []struct {
				Title        string `json:"title"`
				Date         string `json:"date"`
				ReleaseGroup struct {
					ID string `json:"id"`
				} `json:"release-group"`
			} `json:"releases"`
		} `json:"recordings"`
	}
	if err := searchFetch(ctx, s.r.client, "https://musicbrainz.org/ws/2/recording?"+params.Encode(), &data); err != nil {
		return domain.SearchPage{}, err
	}
	out := domain.SearchPage{Results: []domain.SearchResult{}, HasMore: offset+len(data.Recordings) < data.Count}
	for _, row := range data.Recordings {
		id := strings.ToLower(row.ID)
		title := clean(row.Title, 256)
		if !mbidPattern.MatchString(id) || title == "" {
			continue
		}
		// The earliest dated release names the row; an undated one cannot be
		// called first. ISO dates compare correctly as text, even when partial.
		first := -1
		for i, rel := range row.Releases {
			if rel.Date != "" && (first < 0 || rel.Date < row.Releases[first].Date) {
				first = i
			}
		}
		res := domain.SearchResult{
			MediaType: domain.MediaSong, Provider: "musicbrainz", ProviderID: id,
			Ref:   "https://musicbrainz.org/recording/" + id,
			Title: title,
		}
		var length, release string
		if row.Length > 0 {
			secs := (row.Length + 500) / 1000
			length = fmt.Sprintf("%d:%02d", secs/60, secs%60)
		}
		date := row.FirstRelease
		if first >= 0 {
			rel := row.Releases[first]
			release = clean(rel.Title, 256)
			if date == "" {
				date = rel.Date
			}
			if group := strings.ToLower(rel.ReleaseGroup.ID); mbidPattern.MatchString(group) {
				res.ArtworkURL, res.ArtworkKind = "https://coverartarchive.org/release-group/"+group+"/front-250", domain.ArtworkSquare
			}
		}
		if len(date) >= 4 {
			if year, err := strconv.Atoi(date[:4]); err == nil && year > 0 {
				res.Year = year
			}
		}
		res.Subtitle = strings.Join(nonEmpty(clean(creditNames(row.ArtistCredit), 256), length, release), " · ")
		out.Results = append(out.Results, res)
	}
	return out, nil
}

// creditNames joins an artist credit the way MusicBrainz displays it: each
// name followed by its join phrase ("Alpha feat. Beta"). At most 10 count.
func creditNames(credits []credit) string {
	var b strings.Builder
	for i, c := range credits {
		if i >= 10 {
			break
		}
		name := c.Name
		if name == "" {
			name = c.Artist.Name
		}
		b.WriteString(name + c.JoinPhrase)
	}
	return b.String()
}
