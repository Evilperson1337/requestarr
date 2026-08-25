package domain

import (
	"strings"
	"testing"
)

func TestMatchEvidenceAllValidMethodConfidenceCombinations(t *testing.T) {
	methods := []MatchMethod{MatchExternalID, MatchASINRegion, MatchTitleAuthorNarrator, MatchTitleAuthor, MatchTitleYear, MatchManual}
	confidences := []MatchConfidence{MatchExact, MatchHigh, MatchPossible}
	for _, method := range methods {
		for _, confidence := range confidences {
			t.Run(string(method)+"/"+string(confidence), func(t *testing.T) {
				evidence := MatchEvidence{Method: method, Confidence: confidence, Reason: " normalized explanation ", Attributes: []MatchAttribute{{Key: "title.normalized", Value: "example"}}}
				got, err := NormalizeMatchEvidence(evidence)
				if err != nil {
					t.Fatalf("NormalizeMatchEvidence() error = %v", err)
				}
				if got.Reason != "normalized explanation" {
					t.Fatalf("Reason = %q", got.Reason)
				}
				wantAuto := confidence == MatchExact || confidence == MatchHigh
				if got.CanAutoFulfill() != wantAuto {
					t.Fatalf("CanAutoFulfill() = %v, want %v", got.CanAutoFulfill(), wantAuto)
				}
			})
		}
	}
}

func TestNormalizeMatchEvidenceRejectsInvalidOrUnsafeValues(t *testing.T) {
	tests := []MatchEvidence{
		{Method: "unknown", Confidence: MatchExact},
		{Method: MatchManual, Confidence: "certain"},
		{Method: MatchManual, Confidence: MatchHigh, Reason: "unsafe\x00reason"},
		{Method: MatchManual, Confidence: MatchHigh, Reason: strings.Repeat("a", maxMatchReason+1)},
		{Method: MatchManual, Confidence: MatchHigh, Attributes: make([]MatchAttribute, maxMatchAttributes+1)},
		{Method: MatchManual, Confidence: MatchHigh, Attributes: []MatchAttribute{{Key: "Bad Key", Value: "value"}}},
		{Method: MatchManual, Confidence: MatchHigh, Attributes: []MatchAttribute{{Key: "safe", Value: "value\nunsafe"}}},
	}
	for _, evidence := range tests {
		if _, err := NormalizeMatchEvidence(evidence); err == nil {
			t.Fatalf("NormalizeMatchEvidence(%#v) error = nil", evidence)
		}
		if evidence.CanAutoFulfill() {
			t.Fatalf("invalid evidence %#v must not auto-fulfill", evidence)
		}
	}
}
