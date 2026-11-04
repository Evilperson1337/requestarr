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
	// Local is set by the API from the local database, never by a provider.
	Local *SearchLocal `json:"local,omitempty"`
}

// SearchKey names a provider item without a catalog row. Media type is part of
// the key because movie and TV IDs are separate namespaces.
type SearchKey struct {
	MediaType  MediaType
	Provider   string
	ProviderID string
}

// SearchLocal is what this instance already knows about a search result. Its
// absence means "never resolved here", not "missing". It is advisory: the write
// paths enforce everything again.
type SearchLocal struct {
	CatalogID     string        `json:"catalogId,omitempty"`
	RequestStatus RequestStatus `json:"requestStatus,omitempty"`
	Availability  Availability  `json:"availability"`
}

// SearchPage is one page of results. HasMore stands in for a total count,
// which not every provider can give cheaply.
type SearchPage struct {
	Results []SearchResult `json:"results"`
	HasMore bool           `json:"hasMore"`
}
