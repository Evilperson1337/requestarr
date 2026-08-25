package domain

import "testing"

func TestMatchEvidenceCanAutoFulfill(t *testing.T) {
	tests := []struct {
		name       string
		confidence MatchConfidence
		want       bool
	}{
		{name: "exact", confidence: MatchExact, want: true},
		{name: "high", confidence: MatchHigh, want: true},
		{name: "possible", confidence: MatchPossible, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := (MatchEvidence{Confidence: tt.confidence}).CanAutoFulfill()
			if got != tt.want {
				t.Fatalf("CanAutoFulfill() = %v, want %v", got, tt.want)
			}
		})
	}
}
