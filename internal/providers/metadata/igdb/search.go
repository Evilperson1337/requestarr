package igdb

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/providers/metadata"
)

const (
	// searchPageSize is how many games one page shows. IGDB pages by limit and
	// offset, so page N starts at (N-1)*searchPageSize.
	searchPageSize = 20
	// maxSearchesInFlight keeps name searches well inside IGDB's limit of 4
	// requests per second and 8 open requests, which resolves share.
	maxSearchesInFlight = 3
	// searchKinds is the fixed visibility rule: main games, remakes, remasters,
	// expanded games and ports. DLC, expansions, bundles, updates and the rest
	// are hidden. The numbers are IGDB's game_type ids.
	searchKinds  = "0,8,9,10,11"
	searchFields = "fields slug,name,first_release_date,game_type,cover.image_id,platforms.name,platforms.abbreviation;"
)

// escapeSearchText makes user text safe inside an Apicalypse string literal:
// backslash and double quote are escaped, and control characters (which could
// end the statement) become spaces.
func escapeSearchText(text string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(text) {
		switch {
		case r == '\\' || r == '"':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r < 0x20 || r == 0x7f:
			b.WriteByte(' ')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// searchQuery builds the whole request body. Everything but the escaped text is
// a constant or an integer this package computed.
func searchQuery(text string, page int) string {
	offset := (max(page, 1) - 1) * searchPageSize
	// One extra row tells whether another page exists.
	return fmt.Sprintf(`search "%s"; %s where game_type = (%s); limit %d; offset %d;`, escapeSearchText(text), searchFields, searchKinds, searchPageSize+1, offset)
}

// Search finds games by name. Refs are the igdb.com page URLs the resolver reads.
func (r *Resolver) Search(ctx context.Context, q metadata.SearchQuery) (domain.SearchPage, error) {
	if r.clientID == "" || r.secret == "" {
		return domain.SearchPage{}, metadata.ErrNotConfigured
	}
	select {
	case r.slots <- struct{}{}:
		defer func() { <-r.slots }()
	case <-time.After(r.searchWait):
		return domain.SearchPage{}, fmt.Errorf("IGDB searches are at their concurrency cap: %w", metadata.ErrBusy)
	case <-ctx.Done():
		return domain.SearchPage{}, ctx.Err()
	}
	rows, err := r.fetchRows(ctx, searchQuery(q.Text, q.Page), false)
	if err != nil {
		return domain.SearchPage{}, err
	}
	out := domain.SearchPage{Results: []domain.SearchResult{}, HasMore: len(rows) > searchPageSize}
	for i, g := range rows {
		if i >= searchPageSize {
			break
		}
		// The query already asks for these kinds; a row without one, or with
		// another, is dropped rather than trusted.
		name := clean(g.Name, 256)
		if g.ID == 0 || name == "" || !slugPattern.MatchString(g.Slug) || g.GameType == nil || !shownKind(*g.GameType) {
			continue
		}
		res := domain.SearchResult{
			MediaType: domain.MediaGame, Provider: "igdb", ProviderID: strconv.FormatUint(g.ID, 10),
			Ref: "https://www.igdb.com/games/" + g.Slug, Title: name, ArtworkKind: domain.ArtworkPoster, Subtitle: platformHint(g),
		}
		if g.FirstRelease > 0 {
			res.Year = time.Unix(g.FirstRelease, 0).UTC().Year()
		}
		if image := validImageID(g.Cover.ImageID); image != "" {
			res.ArtworkURL = "https://images.igdb.com/igdb/image/upload/t_cover_big/" + image + ".jpg"
		}
		out.Results = append(out.Results, res)
	}
	return out, nil
}

func shownKind(kind int) bool {
	for _, k := range strings.Split(searchKinds, ",") {
		if n, _ := strconv.Atoi(k); n == kind {
			return true
		}
	}
	return false
}

// platformHint is a short "PC, PS4, XONE +1": abbreviations where IGDB has
// them, at most three, then a count of the rest. Platforms are chosen later,
// in the preview.
func platformHint(g gameRow) string {
	var names []string
	for _, p := range g.Platforms {
		name := clean(p.Abbreviation, 24)
		if name == "" {
			name = clean(p.Name, 40)
		}
		if name != "" {
			names = append(names, name)
		}
	}
	if len(names) <= 3 {
		return strings.Join(names, ", ")
	}
	return fmt.Sprintf("%s +%d", strings.Join(names[:3], ", "), len(names)-3)
}
