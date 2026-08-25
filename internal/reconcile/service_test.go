package reconcile

import (
	"context"
	"errors"
	"testing"
	"time"

	"git.example.com/evilperson1337/Requestarr/internal/domain"
)

type fakeEvidenceRepository struct{ activations int }

func (f *fakeEvidenceRepository) Activate(_ context.Context, _ Activation) (Change, error) {
	f.activations++
	return Change{AssociationID: "match", Changed: true}, nil
}
func (f *fakeEvidenceRepository) Deactivate(_ context.Context, _ Target, _ time.Time) (Change, error) {
	return Change{}, nil
}

func TestServicePolicy(t *testing.T) {
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	target := Target{RequestID: "r", SourceID: "s", InventoryItemID: "i"}
	cases := []struct {
		name              string
		evidence          domain.MatchEvidence
		narrator, edition bool
		want              AvailabilityState
		auto              bool
	}{
		{"exact", domain.MatchEvidence{Method: domain.MatchASINRegion, Confidence: domain.MatchExact}, false, false, AvailabilitySatisfied, true},
		{"high", domain.MatchEvidence{Method: domain.MatchTitleAuthorNarrator, Confidence: domain.MatchHigh}, false, false, AvailabilitySatisfied, true},
		{"possible", domain.MatchEvidence{Method: domain.MatchTitleAuthor, Confidence: domain.MatchPossible}, false, false, AvailabilityReview, false},
		{"narrator conflict", domain.MatchEvidence{Method: domain.MatchTitleAuthorNarrator, Confidence: domain.MatchHigh}, true, false, AvailabilityReview, false},
		{"edition conflict", domain.MatchEvidence{Method: domain.MatchASINRegion, Confidence: domain.MatchExact}, false, true, AvailabilityReview, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeEvidenceRepository{}
			got, _, err := NewService(fake).Reconcile(context.Background(), Input{Target: target, Candidate: Candidate{Evidence: tc.evidence, NarratorConflict: tc.narrator, EditionConflict: tc.edition, ObservedAt: now}})
			if err != nil || got.Availability != tc.want || got.AutoFulfill != tc.auto || fake.activations != 1 {
				t.Fatalf("outcome=%+v activations=%d err=%v", got, fake.activations, err)
			}
		})
	}
}

func TestServiceRejectsInvalidEvidenceAndDoesNotDeactivateOnOutage(t *testing.T) {
	fake := &fakeEvidenceRepository{}
	target := Target{RequestID: "r", SourceID: "s", InventoryItemID: "i"}
	_, _, err := NewService(fake).Reconcile(context.Background(), Input{Target: target, Candidate: Candidate{Evidence: domain.MatchEvidence{Method: "bad", Confidence: domain.MatchHigh}, ObservedAt: time.Now()}})
	if !errors.Is(err, ErrInvalidInput) || fake.activations != 0 {
		t.Fatalf("err=%v activations=%d", err, fake.activations)
	}
	got, change, err := NewService(fake).Reconcile(context.Background(), Input{Target: target, SourceFailed: true})
	if err != nil || got.Availability != AvailabilityUnchanged || change.Changed || fake.activations != 0 {
		t.Fatalf("outcome=%+v change=%+v err=%v", got, change, err)
	}
}
