package domain

// AudiobookDetails contains normalized, provider-neutral audiobook-edition
// fields used by request presentation and safe reconciliation. Provider-specific
// payloads belong in adapters and/or CatalogItem.Metadata, not in this domain type.
//
// Requestarr intentionally distinguishes a broader written work from an audiobook
// manifestation conceptually, but V1 does not introduce a full ebook/work schema.
// These fields preserve enough edition identity to evolve toward that model later.
type AudiobookDetails struct {
	ASIN           string            `json:"asin,omitempty"`
	AudibleRegion  string            `json:"audibleRegion,omitempty"`
	Authors        []string          `json:"authors,omitempty"`
	Narrators      []string          `json:"narrators,omitempty"`
	Series         string            `json:"series,omitempty"`
	SeriesPosition string            `json:"seriesPosition,omitempty"`
	RuntimeSeconds int               `json:"runtimeSeconds,omitempty"`
	Language       string            `json:"language,omitempty"`
	Publisher      string            `json:"publisher,omitempty"`
	ReleaseDate    string            `json:"releaseDate,omitempty"`
	EditionType    string            `json:"editionType,omitempty"` // e.g. standard, dramatized/full-cast when provider supplies it
	Abridged       *bool             `json:"abridged,omitempty"`
	AlternateIDs   map[string]string `json:"alternateIds,omitempty"`
}
