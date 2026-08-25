package inventory

import (
	"context"
	"errors"
	"testing"
)

func TestAcceptedObservationFingerprintIsOrderIndependent(t *testing.T) {
	t.Parallel()
	scope := testObservationScope()
	command := AcceptedObservationCommit{
		ID: "abcdef00-0000-4000-8000-000000000010",
		Observation: CompleteObservation{
			Scope:    scope,
			Coverage: completeCoverage(scope),
			Items: []ObservedItem{
				testObservedItem("library-a", "two", "two", "2"),
				testObservedItem("library-a", "one", "one", "1"),
			},
		},
	}
	first, err := AcceptedObservationFingerprint(command)
	if err != nil {
		t.Fatal(err)
	}
	command.Observation.Scope = reorderedScope(scope)
	command.Observation.Coverage[0], command.Observation.Coverage[3] = command.Observation.Coverage[3], command.Observation.Coverage[0]
	command.Observation.Items[0], command.Observation.Items[1] = command.Observation.Items[1], command.Observation.Items[0]
	second, err := AcceptedObservationFingerprint(command)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("fingerprints differ: %x != %x", first, second)
	}
}

func TestAcceptedObservationFingerprintDoesNotIncludeServerAssignedAcceptanceTime(t *testing.T) {
	t.Parallel()
	command := AcceptedObservationCommit{
		ID: "abcdef00-0000-4000-8000-000000000010",
		Observation: CompleteObservation{
			Scope:    testObservationScope(),
			Coverage: completeCoverage(testObservationScope()),
		},
	}
	if _, err := AcceptedObservationFingerprint(command); err != nil {
		t.Fatalf("server-assigned acceptance time must not be required for command identity: %v", err)
	}
}

func TestAcceptedObservationServiceRejectsIncompleteBeforeRepository(t *testing.T) {
	t.Parallel()
	repository := &acceptedObservationRepositoryFake{}
	service := NewAcceptedObservationService(repository)
	_, err := service.Commit(t.Context(), AcceptedObservationCommit{ID: "abcdef00-0000-4000-8000-000000000010", Observation: CompleteObservation{Scope: testObservationScope()}})
	if !errors.Is(err, ErrInvalidObservationCommit) || repository.called {
		t.Fatalf("error=%v called=%v", err, repository.called)
	}
}

type acceptedObservationRepositoryFake struct{ called bool }

func (f *acceptedObservationRepositoryFake) CommitAcceptedObservation(_ context.Context, command AcceptedObservationCommit) (AcceptedObservationCommitResult, error) {
	f.called = true
	return AcceptedObservationCommitResult{ID: command.ID, Applied: true}, nil
}
