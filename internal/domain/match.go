package domain

import (
	"fmt"
	"regexp"
)

const (
	maxMatchReason     = 512
	maxMatchAttributes = 16
)

var matchAttributeKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,47}$`)

// MatchConfidence describes how safely a reconciliation result may affect
// availability. Exact and high confidence are automatic; possible is review-only.
type MatchConfidence string

const (
	MatchExact    MatchConfidence = "exact"
	MatchHigh     MatchConfidence = "high"
	MatchPossible MatchConfidence = "possible"
)

// MatchMethod records the bounded, cross-media basis for a reconciliation match.
type MatchMethod string

const (
	MatchExternalID          MatchMethod = "external_id"
	MatchASINRegion          MatchMethod = "asin_region"
	MatchTitleAuthorNarrator MatchMethod = "title_author_narrator"
	MatchTitleAuthor         MatchMethod = "title_author"
	MatchTitleYear           MatchMethod = "title_year"
	MatchManual              MatchMethod = "manual"
)

// MatchAttribute is bounded, non-secret explanatory evidence. It must never
// contain raw provider payloads, credentials, tokens, or other unbounded data.
type MatchAttribute struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// MatchEvidence is normalized reconciliation provenance.
type MatchEvidence struct {
	Method     MatchMethod      `json:"method"`
	Confidence MatchConfidence  `json:"confidence"`
	Reason     string           `json:"reason,omitempty"`
	Attributes []MatchAttribute `json:"attributes,omitempty"`
}

// NormalizeMatchEvidence validates and canonicalizes bounded match provenance.
func NormalizeMatchEvidence(evidence MatchEvidence) (MatchEvidence, error) {
	if !evidence.Method.Valid() {
		return MatchEvidence{}, fmt.Errorf("match method is invalid")
	}
	if !evidence.Confidence.Valid() {
		return MatchEvidence{}, fmt.Errorf("match confidence is invalid")
	}
	var err error
	if evidence.Reason, err = normalizeText(evidence.Reason, maxMatchReason, "match reason"); err != nil {
		return MatchEvidence{}, err
	}
	if len(evidence.Attributes) > maxMatchAttributes {
		return MatchEvidence{}, fmt.Errorf("too many match attributes")
	}
	for i := range evidence.Attributes {
		attribute := &evidence.Attributes[i]
		attribute.Key, err = normalizeText(attribute.Key, 48, "match attribute key")
		if err != nil || !matchAttributeKeyPattern.MatchString(attribute.Key) {
			return MatchEvidence{}, fmt.Errorf("match attribute key is invalid")
		}
		attribute.Value, err = normalizeText(attribute.Value, 256, "match attribute value")
		if err != nil || attribute.Value == "" {
			return MatchEvidence{}, fmt.Errorf("match attribute value is invalid")
		}
	}
	return evidence, nil
}

func (m MatchMethod) Valid() bool {
	switch m {
	case MatchExternalID, MatchASINRegion, MatchTitleAuthorNarrator, MatchTitleAuthor, MatchTitleYear, MatchManual:
		return true
	default:
		return false
	}
}

func (c MatchConfidence) Valid() bool {
	return c == MatchExact || c == MatchHigh || c == MatchPossible
}

// CanAutoFulfill reports whether valid evidence meets D019's automatic
// fulfillment threshold. Possible matches always require review.
func (e MatchEvidence) CanAutoFulfill() bool {
	if _, err := NormalizeMatchEvidence(e); err != nil {
		return false
	}
	return e.Confidence == MatchExact || e.Confidence == MatchHigh
}
