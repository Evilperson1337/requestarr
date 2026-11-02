// Package audible resolves an audiobook's ASIN into a catalog item (D031).
// Neither upstream is a documented, contractual API: Audnexus is a community
// aggregator and Audible's catalog endpoint is undocumented. So the ASIN is the
// identity and everything fetched is best-effort display data; if both
// upstreams fail, the ASIN is still accepted with a placeholder title.
package audible

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/providers/metadata"
)

const (
	audnexusBase = "https://api.audnex.us"
	// catalogGroups are the Audible response groups that carry what the preview shows.
	catalogGroups = "contributors,media,product_attrs,product_desc,series"
)

// Resolver resolves Audible product URLs and, through Canonicalize, bare ASINs.
type Resolver struct {
	client        *http.Client
	defaultRegion domain.AudibleRegion
	audnexus      string
	catalog       func(domain.AudibleRegion) string
}

// New returns a Resolver that assumes defaultRegion for a bare ASIN. An unknown
// region falls back to the US marketplace.
func New(defaultRegion domain.AudibleRegion, client *http.Client) *Resolver {
	if client == nil {
		client = &http.Client{Timeout: 9 * time.Second}
	}
	if !defaultRegion.Valid() {
		defaultRegion = domain.RegionUS
	}
	return &Resolver{client: client, defaultRegion: defaultRegion, audnexus: audnexusBase,
		catalog: func(r domain.AudibleRegion) string { return "https://api." + r.Host() }}
}

func (*Resolver) Name() string { return "audible" }

func (*Resolver) CanResolve(u *url.URL) bool {
	_, _, err := domain.ParseAudibleURL(u)
	return err == nil
}

// Canonicalize turns a bare ASIN or an Audible product link (tracking
// parameters and all) into https://www.<storefront>/pd/<ASIN>.
func (r *Resolver) Canonicalize(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if asin, err := domain.ParseASIN(raw); err == nil {
		return productURL(asin, r.defaultRegion), true
	}
	if len(raw) > 2048 {
		return "", false
	}
	if u, err := url.Parse(raw); err == nil {
		if asin, region, err := domain.ParseAudibleURL(u); err == nil {
			return productURL(asin, region), true
		}
	}
	return "", false
}

func productURL(asin string, region domain.AudibleRegion) string {
	return "https://www." + region.Host() + "/pd/" + asin
}

func (r *Resolver) Resolve(ctx context.Context, u *url.URL) (domain.ResolvedItem, error) {
	asin, region, err := domain.ParseAudibleURL(u)
	if err != nil {
		return domain.ResolvedItem{}, err
	}
	book, nexusErr := r.fromAudnexus(ctx, asin, region)
	if nexusErr == nil {
		return toResolved(asin, region, book), nil
	}
	book, catalogErr := r.fromCatalog(ctx, asin, region)
	if catalogErr == nil {
		return toResolved(asin, region, book), nil
	}
	if ctx.Err() != nil {
		return domain.ResolvedItem{}, ctx.Err()
	}
	if errors.Is(nexusErr, metadata.ErrNotFound) && errors.Is(catalogErr, metadata.ErrNotFound) {
		return domain.ResolvedItem{}, fmt.Errorf("no audiobook with ASIN %s in the %s marketplace; try the Audible link for another marketplace: %w", asin, strings.ToUpper(string(region)), metadata.ErrNotFound)
	}
	// An unreachable or misbehaving upstream is not the requester's problem.
	return placeholder(asin, region), nil
}

// book is what either upstream yields, before validation.
type book struct {
	Title, Subtitle, Image, Format, Language, Release, Series string
	SeriesPosition                                            string
	Authors, Narrators                                        []string
	RuntimeMin                                                int
}

type audnexusBook struct {
	Title    string `json:"title"`
	Subtitle string `json:"subtitle"`
	Authors  []struct {
		Name string `json:"name"`
	} `json:"authors"`
	Narrators []struct {
		Name string `json:"name"`
	} `json:"narrators"`
	Image         string `json:"image"`
	FormatType    string `json:"formatType"`
	Language      string `json:"language"`
	ReleaseDate   string `json:"releaseDate"`
	RuntimeLength int    `json:"runtimeLengthMin"`
	SeriesPrimary *struct {
		Name     string `json:"name"`
		Position string `json:"position"`
	} `json:"seriesPrimary"`
}

func (r *Resolver) fromAudnexus(ctx context.Context, asin string, region domain.AudibleRegion) (book, error) {
	var out audnexusBook
	endpoint := r.audnexus + "/books/" + url.PathEscape(asin) + "?region=" + url.QueryEscape(string(region))
	if err := metadata.FetchRetrying(ctx, r.client, endpoint, nil, &out); err != nil {
		return book{}, err
	}
	if strings.TrimSpace(out.Title) == "" {
		return book{}, errors.New("Audnexus returned no title")
	}
	b := book{Title: out.Title, Subtitle: out.Subtitle, Image: out.Image, Format: out.FormatType, Language: out.Language, Release: out.ReleaseDate, RuntimeMin: out.RuntimeLength}
	for _, a := range out.Authors {
		b.Authors = append(b.Authors, a.Name)
	}
	for _, n := range out.Narrators {
		b.Narrators = append(b.Narrators, n.Name)
	}
	if out.SeriesPrimary != nil {
		b.Series, b.SeriesPosition = out.SeriesPrimary.Name, out.SeriesPrimary.Position
	}
	return b, nil
}

type catalogResponse struct {
	Product struct {
		ASIN     string `json:"asin"`
		Title    string `json:"title"`
		Subtitle string `json:"subtitle"`
		Authors  []struct {
			Name string `json:"name"`
		} `json:"authors"`
		Narrators []struct {
			Name string `json:"name"`
		} `json:"narrators"`
		Images        map[string]string `json:"product_images"`
		FormatType    string            `json:"format_type"`
		Language      string            `json:"language"`
		ReleaseDate   string            `json:"release_date"`
		RuntimeLength int               `json:"runtime_length_min"`
		Series        []struct {
			Title    string `json:"title"`
			Sequence string `json:"sequence"`
		} `json:"series"`
	} `json:"product"`
}

func (r *Resolver) fromCatalog(ctx context.Context, asin string, region domain.AudibleRegion) (book, error) {
	var out catalogResponse
	endpoint := r.catalog(region) + "/1.0/catalog/products/" + url.PathEscape(asin) + "?response_groups=" + url.QueryEscape(catalogGroups) + "&image_sizes=500"
	if err := metadata.FetchRetrying(ctx, r.client, endpoint, nil, &out); err != nil {
		return book{}, err
	}
	p := out.Product
	if strings.TrimSpace(p.Title) == "" || (p.ASIN != "" && !strings.EqualFold(p.ASIN, asin)) {
		return book{}, errors.New("Audible returned no matching product")
	}
	b := book{Title: p.Title, Subtitle: p.Subtitle, Format: p.FormatType, Language: p.Language, Release: p.ReleaseDate, RuntimeMin: p.RuntimeLength, Image: p.Images["500"]}
	for _, a := range p.Authors {
		b.Authors = append(b.Authors, a.Name)
	}
	for _, n := range p.Narrators {
		b.Narrators = append(b.Narrators, n.Name)
	}
	if len(p.Series) > 0 {
		b.Series, b.SeriesPosition = p.Series[0].Title, p.Series[0].Sequence
	}
	return b, nil
}

func toResolved(asin string, region domain.AudibleRegion, b book) domain.ResolvedItem {
	meta := map[string]any{"asin": asin, "region": string(region)}
	if names := cleanList(b.Authors, 10); len(names) > 0 {
		meta["authors"] = names
	}
	if names := cleanList(b.Narrators, 10); len(names) > 0 {
		meta["narrators"] = names
	}
	if s := clean(b.Subtitle, 512); s != "" {
		meta["subtitle"] = s
	}
	switch strings.ToLower(strings.TrimSpace(b.Format)) {
	case "abridged":
		meta["abridged"] = true
	case "unabridged":
		meta["abridged"] = false
	}
	if l := clean(b.Language, 40); l != "" {
		meta["language"] = l
	}
	if b.RuntimeMin > 0 && b.RuntimeMin < 100000 {
		meta["runtimeMinutes"] = b.RuntimeMin
	}
	if s := clean(b.Series, 256); s != "" {
		meta["series"] = s
		if pos := clean(b.SeriesPosition, 32); pos != "" {
			meta["seriesPosition"] = pos
		}
	}
	item := domain.CatalogItem{MediaType: domain.MediaAudiobook, Provider: "audible", ProviderID: asin, Title: clean(b.Title, 512),
		ArtworkKind: domain.ArtworkSquare, ArtworkURL: validCover(b.Image), ExternalURL: productURL(asin, region), Metadata: meta}
	if len(b.Release) >= 4 {
		if y, err := strconv.Atoi(b.Release[:4]); err == nil && y > 1800 && y < 3000 {
			item.Year = y
		}
	}
	return domain.ResolvedItem{Item: item}
}

// placeholder accepts the ASIN when no upstream answered. It is marked
// unresolved so the catalog never lets it replace real metadata (D031).
func placeholder(asin string, region domain.AudibleRegion) domain.ResolvedItem {
	return domain.ResolvedItem{Item: domain.CatalogItem{MediaType: domain.MediaAudiobook, Provider: "audible", ProviderID: asin, Title: "Audiobook " + asin,
		ArtworkKind: domain.ArtworkSquare, ExternalURL: productURL(asin, region),
		Metadata: map[string]any{"asin": asin, "region": string(region), "unresolved": true}}}
}

// validCover admits only an https image on Amazon's image hosts.
func validCover(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || len(raw) > 2048 {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if host == "m.media-amazon.com" || strings.HasSuffix(host, ".ssl-images-amazon.com") || strings.HasSuffix(host, ".media-amazon.com") {
		return raw
	}
	return ""
}

func cleanList(in []string, limit int) []string {
	var out []string
	for _, s := range in {
		if s = clean(s, 256); s != "" && len(out) < limit {
			out = append(out, s)
		}
	}
	return out
}

func clean(s string, limit int) string {
	s = strings.TrimSpace(strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s))
	if len(s) > limit {
		s = strings.ToValidUTF8(s[:limit], "")
	}
	return s
}
