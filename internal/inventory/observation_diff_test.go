package inventory

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"git.example.com/evilperson1337/Requestarr/internal/domain"
)

func TestDiffCompleteObservation_ClassifiesSortedAndDoesNotMutate(t *testing.T) {
	t.Parallel()
	scope := testObservationScope()
	known := KnownInventoryState{Scope: scope, Items: []ObservedItem{testObservedItem("library-b", "remove", "old", "1"), testObservedItem("library-a", "same", "same", "1"), testObservedItem("library-a", "update", "old", "1")}}
	observation := CompleteObservation{Scope: reorderedScope(scope), Coverage: completeCoverage(scope), Items: []ObservedItem{testObservedItem("library-b", "add-z", "added", "1"), testObservedItem("library-a", "update", "new", "2"), testObservedItem("library-a", "add-a", "added", "1"), testObservedItem("library-a", "same", "same", "1")}}
	wantKnown, wantObservation := cloneKnown(known), cloneObservation(observation)

	diff, err := DiffCompleteObservation(observation, known)
	if err != nil {
		t.Fatalf("DiffCompleteObservation() error = %v", err)
	}
	if !reflect.DeepEqual(known, wantKnown) || !reflect.DeepEqual(observation, wantObservation) {
		t.Fatal("DiffCompleteObservation() mutated input")
	}
	assertItemIDs(t, diff.Unchanged, "same")
	assertItemIDs(t, diff.Added, "add-a", "add-z")
	assertItemIDs(t, diff.Updated, "update")
	assertItemIDs(t, diff.Removed, "remove")
	diff.Added[0].Title = "mutated output"
	if observation.Items[2].Title == "mutated output" || known.Items[0].Title == "mutated output" {
		t.Fatal("diff aliases caller-owned items")
	}
}

func TestDiffCompleteObservation_ProvenEmptyRemovesAll(t *testing.T) {
	t.Parallel()
	scope := testObservationScope()
	coverage := completeCoverage(scope)
	coverage[1].Status = CoverageInapplicable
	diff, err := DiffCompleteObservation(CompleteObservation{Scope: scope, Coverage: coverage}, KnownInventoryState{Scope: scope, Items: []ObservedItem{testObservedItem("library-a", "one", "one", "1"), testObservedItem("library-b", "two", "two", "1")}})
	if err != nil {
		t.Fatalf("DiffCompleteObservation() error = %v", err)
	}
	assertItemIDs(t, diff.Removed, "one", "two")
}

func TestDiffCompleteObservation_FailsClosed(t *testing.T) {
	t.Parallel()
	scope := testObservationScope()
	tests := []struct {
		name        string
		observation CompleteObservation
		known       KnownInventoryState
		want        error
	}{
		{"missing coverage", CompleteObservation{Scope: scope}, KnownInventoryState{Scope: scope}, ErrObservationIncomplete},
		{"source mismatch", CompleteObservation{Scope: scope, Coverage: completeCoverage(scope)}, KnownInventoryState{Scope: withSource(scope, "00000000-0000-4000-8000-000000000002")}, ErrObservationScopeMismatch},
		{"revision mismatch", CompleteObservation{Scope: scope, Coverage: completeCoverage(scope)}, KnownInventoryState{Scope: withRevision(scope, 2)}, ErrObservationScopeMismatch},
		{"library mismatch", CompleteObservation{Scope: scope, Coverage: completeCoverage(scope)}, KnownInventoryState{Scope: withLibraries(scope, []string{"library-a"})}, ErrObservationScopeMismatch},
		{"media type mismatch", CompleteObservation{Scope: scope, Coverage: completeCoverage(scope)}, KnownInventoryState{Scope: withMediaTypes(scope, []domain.MediaType{domain.MediaMovie})}, ErrObservationScopeMismatch},
		{"known invalid", CompleteObservation{Scope: scope, Coverage: completeCoverage(scope)}, KnownInventoryState{Scope: scope, Items: []ObservedItem{testObservedItem("library-a", "", "bad", "1")}}, ErrInvalidKnownInventoryState},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DiffCompleteObservation(tt.observation, tt.known)
			if !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want errors.Is(_, %v)", err, tt.want)
			}
		})
	}
}

func TestValidateCompleteObservation_CoveragePartitions(t *testing.T) {
	t.Parallel()
	scope := testObservationScope()
	tests := []struct {
		name        string
		observation CompleteObservation
		want        error
	}{
		{"missing pair", CompleteObservation{Scope: scope, Coverage: completeCoverage(scope)[:3]}, ErrObservationIncomplete},
		{"duplicate pair", CompleteObservation{Scope: scope, Coverage: append(completeCoverage(scope)[:3], completeCoverage(scope)[0])}, ErrObservationIncomplete},
		{"failed", CompleteObservation{Scope: scope, Coverage: []ObservationCoverage{{"library-a", domain.MediaMovie, "failed"}, {"library-a", domain.MediaTV, CoverageVerifiedComplete}, {"library-b", domain.MediaMovie, CoverageVerifiedComplete}, {"library-b", domain.MediaTV, CoverageVerifiedComplete}}}, ErrObservationIncomplete},
		{"out of scope library", CompleteObservation{Scope: scope, Coverage: []ObservationCoverage{{"other", domain.MediaMovie, CoverageVerifiedComplete}, {"library-a", domain.MediaTV, CoverageVerifiedComplete}, {"library-b", domain.MediaMovie, CoverageVerifiedComplete}, {"library-b", domain.MediaTV, CoverageVerifiedComplete}}}, ErrObservationIncomplete},
		{"out of scope type", CompleteObservation{Scope: scope, Coverage: []ObservationCoverage{{"library-a", domain.MediaGame, CoverageVerifiedComplete}, {"library-a", domain.MediaTV, CoverageVerifiedComplete}, {"library-b", domain.MediaMovie, CoverageVerifiedComplete}, {"library-b", domain.MediaTV, CoverageVerifiedComplete}}}, ErrObservationIncomplete},
		{"item inapplicable", CompleteObservation{Scope: scope, Coverage: []ObservationCoverage{{"library-a", domain.MediaMovie, CoverageInapplicable}, {"library-a", domain.MediaTV, CoverageVerifiedComplete}, {"library-b", domain.MediaMovie, CoverageVerifiedComplete}, {"library-b", domain.MediaTV, CoverageVerifiedComplete}}, Items: []ObservedItem{testObservedItem("library-a", "id", "one", "1")}}, ErrInvalidCompleteObservation},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateCompleteObservation(tt.observation); !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want errors.Is(_, %v)", err, tt.want)
			}
		})
	}
}

func TestValidateCompleteObservation_RejectsNonCanonicalAndInvalidData(t *testing.T) {
	t.Parallel()
	scope := testObservationScope()
	tests := []struct {
		name        string
		observation CompleteObservation
	}{
		{"non UUID source", CompleteObservation{Scope: withSource(scope, "source-a"), Coverage: completeCoverage(scope)}},
		{"uppercase UUID", CompleteObservation{Scope: withSource(scope, strings.ToUpper(scope.SourceID)), Coverage: completeCoverage(scope)}},
		{"duplicate selected library", CompleteObservation{Scope: withLibraries(scope, []string{"library-a", "library-a"}), Coverage: completeCoverage(scope)}},
		{"duplicate enabled type", CompleteObservation{Scope: withMediaTypes(scope, []domain.MediaType{domain.MediaMovie, domain.MediaMovie}), Coverage: completeCoverage(scope)}},
		{"trimmed item equivalence rejected", CompleteObservation{Scope: scope, Coverage: completeCoverage(scope), Items: []ObservedItem{{RemoteLibraryID: "library-a", ItemID: " id ", MediaType: domain.MediaMovie, Title: " title ", Revision: " 1 ", ExternalURL: "https://example.test/item/id"}}}},
		{"noncanonical URL rejected", CompleteObservation{Scope: scope, Coverage: completeCoverage(scope), Items: []ObservedItem{{RemoteLibraryID: "library-a", ItemID: "id", MediaType: domain.MediaMovie, Title: "title", Revision: "1", ExternalURL: " HTTPS://EXAMPLE.TEST/item/id "}}}},
		{"oversized revision", CompleteObservation{Scope: scope, Coverage: completeCoverage(scope), Items: []ObservedItem{{RemoteLibraryID: "library-a", ItemID: "id", MediaType: domain.MediaMovie, Title: "title", Revision: strings.Repeat("x", maxItemRevisionSize+1)}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateCompleteObservation(tt.observation); !errors.Is(err, ErrInvalidCompleteObservation) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func testObservationScope() ObservationScope {
	return ObservationScope{SourceID: "abcdef00-0000-4000-8000-000000000001", ConfigurationRevision: 1, SelectedLibraries: []string{"library-a", "library-b"}, EnabledMediaTypes: []domain.MediaType{domain.MediaMovie, domain.MediaTV}}
}
func reorderedScope(scope ObservationScope) ObservationScope {
	return ObservationScope{SourceID: scope.SourceID, ConfigurationRevision: scope.ConfigurationRevision, SelectedLibraries: []string{"library-b", "library-a"}, EnabledMediaTypes: []domain.MediaType{domain.MediaTV, domain.MediaMovie}}
}
func completeCoverage(scope ObservationScope) []ObservationCoverage {
	var coverage []ObservationCoverage
	for _, library := range scope.SelectedLibraries {
		for _, mediaType := range scope.EnabledMediaTypes {
			coverage = append(coverage, ObservationCoverage{library, mediaType, CoverageVerifiedComplete})
		}
	}
	return coverage
}
func withSource(scope ObservationScope, source string) ObservationScope {
	scope.SourceID = source
	return scope
}
func withRevision(scope ObservationScope, revision int64) ObservationScope {
	scope.ConfigurationRevision = revision
	return scope
}
func withLibraries(scope ObservationScope, libraries []string) ObservationScope {
	scope.SelectedLibraries = libraries
	return scope
}
func withMediaTypes(scope ObservationScope, types []domain.MediaType) ObservationScope {
	scope.EnabledMediaTypes = types
	return scope
}
func testObservedItem(library, id, title, revision string) ObservedItem {
	return ObservedItem{RemoteLibraryID: library, ItemID: id, MediaType: domain.MediaMovie, Title: title, Revision: revision, ExternalURL: "https://example.test/item/" + id}
}
func assertItemIDs(t *testing.T, items []ObservedItem, want ...string) {
	t.Helper()
	got := make([]string, len(items))
	for i := range items {
		got[i] = items[i].ItemID
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("item IDs = %v, want %v", got, want)
	}
}
func cloneKnown(value KnownInventoryState) KnownInventoryState {
	return KnownInventoryState{Scope: cloneScope(value.Scope), Items: append([]ObservedItem(nil), value.Items...)}
}
func cloneObservation(value CompleteObservation) CompleteObservation {
	return CompleteObservation{Scope: cloneScope(value.Scope), Coverage: append([]ObservationCoverage(nil), value.Coverage...), Items: append([]ObservedItem(nil), value.Items...)}
}
func cloneScope(value ObservationScope) ObservationScope {
	value.SelectedLibraries = append([]string(nil), value.SelectedLibraries...)
	value.EnabledMediaTypes = append([]domain.MediaType(nil), value.EnabledMediaTypes...)
	return value
}
