package domain

// SearchResult is a pointer to a provider item found by name. It is not a
// CatalogItem: nothing here is stored. Ref is a value the same provider's
// resolver accepts, so resolving it gives what pasting that link would.
type SearchResult struct {
	MediaType   MediaType   `json:"mediaType"`
	Provider    string      `json:"provider"`
	ProviderID  string      `json:"providerId"`
	Ref         string      `json:"ref"`
	Title       string      `json:"title"`
	Year        int         `json:"year,omitempty"`
	Subtitle    string      `json:"subtitle,omitempty"`
	ArtworkURL  string      `json:"artworkUrl,omitempty"`
	ArtworkKind ArtworkKind `json:"artworkKind,omitempty"`
	// Score is the provider's relevance signal. It orders results and is not exposed.
	Score float64 `json:"-"`
}

// SearchPage is one page of results. HasMore stands in for a total count,
// which not every provider can give cheaply.
type SearchPage struct {
	Results []SearchResult `json:"results"`
	HasMore bool           `json:"hasMore"`
}
