package domain

// MatchConfidence describes how safely Requestarr can use a reconciliation
// result. Automatic availability transitions should require Exact or High
// confidence; Possible matches are review-only.
type MatchConfidence string

const (
	MatchExact    MatchConfidence = "exact"
	MatchHigh     MatchConfidence = "high"
	MatchPossible MatchConfidence = "possible"
)

// MatchMethod records why an inventory item was associated with a catalog
// item/request. Methods are intentionally cross-media so reconciliation can use
// one evidence model for TMDB, MusicBrainz, IGDB, ASIN, and fallback matching.
type MatchMethod string

const (
	MatchExternalID          MatchMethod = "external_id"
	MatchASINRegion          MatchMethod = "asin_region"
	MatchTitleAuthorNarrator MatchMethod = "title_author_narrator"
	MatchTitleAuthor         MatchMethod = "title_author"
	MatchTitleYear           MatchMethod = "title_year"
	MatchManual              MatchMethod = "manual"
)

// MatchEvidence is safe, normalized reconciliation provenance. Evidence must
// never contain provider credentials, access tokens, or unbounded raw payloads.
type MatchEvidence struct {
	Method     MatchMethod     `json:"method"`
	Confidence MatchConfidence `json:"confidence"`
	Reason     string          `json:"reason,omitempty"`
}

// CanAutoFulfill returns whether a match is sufficiently trustworthy to drive
// an automatic request -> available transition.
func (m MatchEvidence) CanAutoFulfill() bool {
	return m.Confidence == MatchExact || m.Confidence == MatchHigh
}
