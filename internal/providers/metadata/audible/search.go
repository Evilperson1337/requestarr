package audible

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/providers/metadata"
)

const (
	searchPageSize = 25
	// searchGroups are the response groups a row needs: narrators and authors,
	// the cover, the runtime and format, and series membership.
	searchGroups = "contributors,media,product_attrs,series"
)

// Search finds audiobooks by name in the admin's default marketplace with one
// call to the undocumented Audible catalog search (see README.md). Audnexus has
// no title search. Two editions of one title are two ASINs and so two rows
// (D031). One attempt only: the shared search service caches answers and the
// fetch is time-bounded.
func (r *Resolver) Search(ctx context.Context, q metadata.SearchQuery) (domain.SearchPage, error) {
	text := clean(strings.Join(strings.Fields(q.Text), " "), 200)
	if text == "" {
		return domain.SearchPage{Results: []domain.SearchResult{}}, nil
	}
	page := max(q.Page, 1)
	params := url.Values{"keywords": {text}, "num_results": {strconv.Itoa(searchPageSize)}, "page": {strconv.Itoa(page - 1)},
		"response_groups": {searchGroups}, "image_sizes": {"500"}, "products_sort_by": {"Relevance"}}
	var data struct {
		Total    int `json:"total_results"`
		Products *[]struct {
			ASIN        string `json:"asin"`
			Title       string `json:"title"`
			Subtitle    string `json:"subtitle"`
			ContentType string `json:"content_type"`
			Delivery    string `json:"content_delivery_type"`
			FormatType  string `json:"format_type"`
			HasChildren bool   `json:"has_children"`
			Runtime     int    `json:"runtime_length_min"`
			Release     string `json:"release_date"`
			Authors     []struct {
				Name string `json:"name"`
			} `json:"authors"`
			Narrators []struct {
				Name string `json:"name"`
			} `json:"narrators"`
			Images map[string]string `json:"product_images"`
		} `json:"products"`
	}
	if err := metadata.Fetch(ctx, r.client, r.catalog(r.defaultRegion)+"/1.0/catalog/products/?"+params.Encode(), nil, &data); err != nil {
		return domain.SearchPage{}, err
	}
	if data.Products == nil {
		// An answer without a product list is a changed or broken endpoint, not "no matches".
		return domain.SearchPage{}, errors.New("Audible catalog search answered without a product list")
	}
	offset := (page - 1) * searchPageSize
	out := domain.SearchPage{Results: []domain.SearchResult{}, HasMore: offset+len(*data.Products) < data.Total}
	for _, p := range *data.Products {
		asin, err := domain.ParseASIN(p.ASIN)
		title := clean(p.Title, 512)
		if err != nil || title == "" || (p.ContentType != "" && p.ContentType != "Product") {
			continue
		}
		var authors, narrators []string
		for _, a := range p.Authors {
			authors = append(authors, a.Name)
		}
		for _, n := range p.Narrators {
			narrators = append(narrators, n.Name)
		}
		detail := nonEmpty(clean(p.Subtitle, 200), strings.Join(cleanList(authors, 3), ", "))
		if names := cleanList(narrators, 3); len(names) > 0 {
			detail = append(detail, "Narrated by "+strings.Join(names, ", "))
		}
		if p.Runtime > 0 && p.Runtime < 100000 {
			detail = append(detail, fmt.Sprintf("%dh %dm", p.Runtime/60, p.Runtime%60))
		}
		switch strings.ToLower(p.FormatType) {
		case "abridged":
			detail = append(detail, "Abridged")
		case "unabridged":
			detail = append(detail, "Unabridged")
		}
		if p.Delivery == "MultiPartBook" || p.HasChildren {
			detail = append(detail, "Series / multi-part")
		}
		res := domain.SearchResult{MediaType: domain.MediaAudiobook, Provider: "audible", ProviderID: asin, Ref: productURL(asin, r.defaultRegion),
			Title: title, Subtitle: strings.Join(detail, " · "), ArtworkURL: validCover(p.Images["500"]), ArtworkKind: domain.ArtworkSquare}
		if len(p.Release) >= 4 {
			if y, err := strconv.Atoi(p.Release[:4]); err == nil && y > 1800 && y < 3000 {
				res.Year = y
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
