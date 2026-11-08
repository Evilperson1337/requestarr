package musicbrainz

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/providers/metadata"
)

const (
	searchPageSize = 25
	maxQueryTerms  = 12
)

// Search finds albums by name with one release-group search. The artist,
// type and first-release year arrive in the same answer, and cover art is only
// a URL derived from the identifier, so nothing here makes a second call. It
// goes through searchFetch, which waits in the shared spacer behind any resolve.
func (r *Resolver) Search(ctx context.Context, q metadata.SearchQuery) (domain.SearchPage, error) {
	query := albumQuery(q.Text, q.IncludeSingles)
	if query == "" {
		return domain.SearchPage{Results: []domain.SearchResult{}}, nil
	}
	page := max(q.Page, 1)
	offset := (page - 1) * searchPageSize
	params := url.Values{"query": {query}, "fmt": {"json"}, "limit": {strconv.Itoa(searchPageSize)}, "offset": {strconv.Itoa(offset)}}
	var data struct {
		Count         int `json:"count"`
		ReleaseGroups []struct {
			ID             string   `json:"id"`
			Title          string   `json:"title"`
			PrimaryType    string   `json:"primary-type"`
			SecondaryTypes []string `json:"secondary-types"`
			FirstRelease   string   `json:"first-release-date"`
			ArtistCredit   []struct {
				Name       string `json:"name"`
				JoinPhrase string `json:"joinphrase"`
				Artist     struct {
					Name string `json:"name"`
				} `json:"artist"`
			} `json:"artist-credit"`
		} `json:"release-groups"`
	}
	if err := searchFetch(ctx, r.client, "https://musicbrainz.org/ws/2/release-group?"+params.Encode(), &data); err != nil {
		return domain.SearchPage{}, err
	}
	out := domain.SearchPage{Results: []domain.SearchResult{}, HasMore: offset+len(data.ReleaseGroups) < data.Count}
	for _, row := range data.ReleaseGroups {
		id := strings.ToLower(row.ID)
		title := clean(row.Title, 256)
		if !mbidPattern.MatchString(id) || title == "" {
			continue
		}
		kind := clean(row.PrimaryType, 40)
		if kind != "" {
			var extra []string
			for _, t := range row.SecondaryTypes {
				if name := clean(t, 40); name != "" && len(extra) < 3 {
					extra = append(extra, name)
				}
			}
			if len(extra) > 0 {
				kind += " (" + strings.Join(extra, ", ") + ")"
			}
		}
		var artists strings.Builder
		for i, c := range row.ArtistCredit {
			name := c.Name
			if name == "" {
				name = c.Artist.Name
			}
			if i < 10 {
				artists.WriteString(name + c.JoinPhrase)
			}
		}
		res := domain.SearchResult{
			MediaType: domain.MediaAlbum, Provider: "musicbrainz", ProviderID: id,
			Ref:         "https://musicbrainz.org/release-group/" + id,
			Title:       title,
			Subtitle:    strings.Join(nonEmpty(kind, clean(artists.String(), 256)), " · "),
			ArtworkURL:  "https://coverartarchive.org/release-group/" + id + "/front-250",
			ArtworkKind: domain.ArtworkSquare,
		}
		if len(row.FirstRelease) >= 4 {
			if year, err := strconv.Atoi(row.FirstRelease[:4]); err == nil && year > 0 {
				res.Year = year
			}
		}
		out.Results = append(out.Results, res)
	}
	return out, nil
}

func nonEmpty(parts ...string) []string {
	var out []string
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// albumQuery matches each typed word against the release group title or the
// artist, limited to albums (or also singles and EPs).
func albumQuery(text string, includeSingles bool) string {
	terms := termsQuery(text, "releasegroup")
	if terms == "" {
		return ""
	}
	kinds := `primarytype:"album"`
	if includeSingles {
		kinds = `(primarytype:"album" OR primarytype:"single" OR primarytype:"ep")`
	}
	return terms + " AND " + kinds
}

// termsQuery builds the Lucene query for a typed name. Every word becomes one
// quoted phrase matched against titleField or the artist, and the words are
// AND-ed, so "pink floyd dark side" finds the album while the text can never
// act as syntax: inside quotes only a backslash and a quote mean anything and
// both are escaped, which also turns AND, OR, NOT and field names into plain
// words. A word with no letter or digit (a lone "*", say) is dropped. An empty
// result means there is nothing to ask.
// searchAttempts bounds how often one name search asks MusicBrainz.
const searchAttempts = 3

// searchFetch runs one name search. MusicBrainz's search servers answer 503
// "busy" now and then even to a client well inside its rate limit (in a live
// measurement, about one recording search in four), so a busy answer is asked
// again, up to searchAttempts times. Every attempt waits for its own slot in
// the search line of the shared spacer, so a retry keeps the one-per-second
// budget and never goes ahead of a preview, which is always served first. The
// shared search service's time budget bounds the whole thing. A full search
// line, a cancelled search and any other failure are not retried.
func searchFetch(ctx context.Context, client *http.Client, endpoint string, dst any) error {
	var err error
	for attempt := 0; attempt < searchAttempts; attempt++ {
		if err = apiSpacer.waitSearch(ctx); err != nil {
			return err
		}
		err = metadata.Fetch(ctx, client, endpoint, nil, dst)
		if err == nil || !errors.Is(err, metadata.ErrBusy) || ctx.Err() != nil {
			return err
		}
	}
	return err
}

func termsQuery(text, titleField string) string {
	var clauses []string
	for _, word := range strings.Fields(text) {
		if len(clauses) >= maxQueryTerms {
			break
		}
		if strings.IndexFunc(word, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }) < 0 {
			continue
		}
		phrase := `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(word) + `"`
		clauses = append(clauses, "("+titleField+":"+phrase+" OR artist:"+phrase+")")
	}
	return strings.Join(clauses, " AND ")
}
