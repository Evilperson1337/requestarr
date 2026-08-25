package inventory

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"git.example.com/evilperson1337/Requestarr/internal/domain"
)

func TestValidateCompleteReaderResult(t *testing.T) {
	requirements := CompleteReaderRequirements{SourceID: "source", SelectedLibraries: []string{"movies"}, EnabledMediaTypes: []domain.MediaType{domain.MediaMovie}}
	valid := CompleteReaderResult{SourceID: "source", CompletedAt: time.Now(), Complete: true, Coverage: []ObservationCoverage{{RemoteLibraryID: "movies", MediaType: domain.MediaMovie, Status: CoverageVerifiedComplete}}, Items: []CompleteReaderItem{{RemoteLibraryID: "movies", SourceItemID: "movies:1", MediaType: domain.MediaMovie, Title: "Title", ExternalIDs: map[string]string{"tmdb": "1"}}}}
	if err := ValidateCompleteReaderResult(valid, requirements); err != nil {
		t.Fatalf("valid result: %v", err)
	}
	for _, mutate := range []func(*CompleteReaderResult, *CompleteReaderRequirements){
		func(result *CompleteReaderResult, _ *CompleteReaderRequirements) { result.Complete = false },
		func(result *CompleteReaderResult, _ *CompleteReaderRequirements) { result.SourceID = "other" },
		func(result *CompleteReaderResult, _ *CompleteReaderRequirements) {
			result.Items[0].RemoteLibraryID = "other"
		},
		func(result *CompleteReaderResult, _ *CompleteReaderRequirements) {
			result.Items[0].MediaType = domain.MediaTV
		},
		func(result *CompleteReaderResult, _ *CompleteReaderRequirements) {
			result.Items = append(result.Items, result.Items[0])
		},
		func(result *CompleteReaderResult, _ *CompleteReaderRequirements) { result.Coverage = nil },
		func(_ *CompleteReaderResult, requirements *CompleteReaderRequirements) {
			requirements.SelectedLibraries = nil
		},
	} {
		result := valid
		result.Items = append([]CompleteReaderItem(nil), valid.Items...)
		requirementCopy := requirements
		requirementCopy.SelectedLibraries = append([]string(nil), requirements.SelectedLibraries...)
		mutate(&result, &requirementCopy)
		if err := ValidateCompleteReaderResult(result, requirementCopy); !errors.Is(err, ErrInvalidCompleteReaderResult) {
			t.Fatalf("invalid result error = %v", err)
		}
	}
}

func TestValidateCompleteReaderResultAllowsEmptyCompleteSelectedInventory(t *testing.T) {
	err := ValidateCompleteReaderResult(CompleteReaderResult{SourceID: "source", CompletedAt: time.Now(), Complete: true, Coverage: []ObservationCoverage{{RemoteLibraryID: "movies", MediaType: domain.MediaMovie, Status: CoverageVerifiedComplete}}}, CompleteReaderRequirements{SourceID: "source", SelectedLibraries: []string{"movies"}, EnabledMediaTypes: []domain.MediaType{domain.MediaMovie}})
	if err != nil {
		t.Fatalf("empty complete result: %v", err)
	}
}

func TestValidateCompleteReaderResult_CoverageIsExact(t *testing.T) {
	t.Parallel()
	requirements := CompleteReaderRequirements{SourceID: "source", SelectedLibraries: []string{"movies", "shows"}, EnabledMediaTypes: []domain.MediaType{domain.MediaMovie, domain.MediaTV}}
	coverage := []ObservationCoverage{{"movies", domain.MediaMovie, CoverageVerifiedComplete}, {"movies", domain.MediaTV, CoverageInapplicable}, {"shows", domain.MediaMovie, CoverageInapplicable}, {"shows", domain.MediaTV, CoverageVerifiedComplete}}
	for _, mutate := range []func(*CompleteReaderResult){
		func(result *CompleteReaderResult) { result.Coverage = result.Coverage[:3] },
		func(result *CompleteReaderResult) { result.Coverage[3] = result.Coverage[0] },
		func(result *CompleteReaderResult) { result.Coverage[0].RemoteLibraryID = "other" },
		func(result *CompleteReaderResult) { result.Coverage[0].Status = "failed" },
	} {
		result := CompleteReaderResult{SourceID: "source", CompletedAt: time.Now(), Complete: true, Coverage: append([]ObservationCoverage(nil), coverage...)}
		mutate(&result)
		if err := ValidateCompleteReaderResult(result, requirements); !errors.Is(err, ErrInvalidCompleteReaderResult) {
			t.Fatalf("error = %v", err)
		}
	}
}

func TestValidateCompleteReaderResult_RawIDMayBeReusedInDifferentLibraries(t *testing.T) {
	t.Parallel()
	requirements := CompleteReaderRequirements{SourceID: "source", SelectedLibraries: []string{"one", "two"}, EnabledMediaTypes: []domain.MediaType{domain.MediaMovie}}
	result := CompleteReaderResult{SourceID: "source", CompletedAt: time.Now(), Complete: true, Coverage: []ObservationCoverage{{"one", domain.MediaMovie, CoverageVerifiedComplete}, {"two", domain.MediaMovie, CoverageVerifiedComplete}}, Items: []CompleteReaderItem{{RemoteLibraryID: "one", SourceItemID: "same", MediaType: domain.MediaMovie, Title: "One"}, {RemoteLibraryID: "two", SourceItemID: "same", MediaType: domain.MediaMovie, Title: "Two"}}}
	if err := ValidateCompleteReaderResult(result, requirements); err != nil {
		t.Fatalf("library-qualified IDs should be valid: %v", err)
	}
}

func TestValidateCompleteReaderResult_RejectsItemInInapplicablePair(t *testing.T) {
	t.Parallel()
	requirements := CompleteReaderRequirements{SourceID: "source", SelectedLibraries: []string{"movies"}, EnabledMediaTypes: []domain.MediaType{domain.MediaMovie}}
	result := CompleteReaderResult{SourceID: "source", CompletedAt: time.Now(), Complete: true, Coverage: []ObservationCoverage{{"movies", domain.MediaMovie, CoverageInapplicable}}, Items: []CompleteReaderItem{{RemoteLibraryID: "movies", SourceItemID: "1", MediaType: domain.MediaMovie, Title: "Title"}}}
	if err := ValidateCompleteReaderResult(result, requirements); !errors.Is(err, ErrInvalidCompleteReaderResult) {
		t.Fatalf("error = %v", err)
	}
}

func TestConvertCompleteReaderResult(t *testing.T) {
	t.Parallel()
	scope := ObservationScope{SourceID: "abcdef00-0000-4000-8000-000000000001", ConfigurationRevision: 2, SelectedLibraries: []string{"movies"}, EnabledMediaTypes: []domain.MediaType{domain.MediaMovie}}
	result := CompleteReaderResult{SourceID: scope.SourceID, CompletedAt: time.Now(), Complete: true, Coverage: []ObservationCoverage{{RemoteLibraryID: "movies", MediaType: domain.MediaMovie, Status: CoverageVerifiedComplete}}, Items: []CompleteReaderItem{{RemoteLibraryID: "movies", SourceItemID: "1", MediaType: domain.MediaMovie, Title: "Title", ExternalURL: " HTTPS://Example.TEST:443/item/1 "}}}
	wantScope, wantResult := cloneObservationScope(scope), result
	wantResult.Coverage = append([]ObservationCoverage(nil), result.Coverage...)
	wantResult.Items = append([]CompleteReaderItem(nil), result.Items...)
	observation, err := ConvertCompleteReaderResult(scope, SourcePlex, result)
	if err != nil {
		t.Fatalf("ConvertCompleteReaderResult() error = %v", err)
	}
	if !reflect.DeepEqual(scope, wantScope) || !reflect.DeepEqual(result, wantResult) {
		t.Fatal("ConvertCompleteReaderResult() mutated input")
	}
	if got, want := observation.Items[0].ExternalURL, "https://example.test:443/item/1"; got != want {
		t.Fatalf("canonical URL = %q, want %q", got, want)
	}
	if observation.Items[0].Revision == "" {
		t.Fatal("revision is empty")
	}
	again, err := ConvertCompleteReaderResult(scope, SourcePlex, result)
	if err != nil || !reflect.DeepEqual(observation, again) {
		t.Fatalf("conversion is not stable: err=%v", err)
	}
}

func TestConvertCompleteReaderResult_KindApplicability(t *testing.T) {
	t.Parallel()
	bookScope := ObservationScope{SourceID: "abcdef00-0000-4000-8000-000000000001", ConfigurationRevision: 1, SelectedLibraries: []string{"books"}, EnabledMediaTypes: []domain.MediaType{domain.MediaAudiobook}}
	bookResult := CompleteReaderResult{SourceID: bookScope.SourceID, CompletedAt: time.Now(), Complete: true, Coverage: []ObservationCoverage{{"books", domain.MediaAudiobook, CoverageVerifiedComplete}}}
	if _, err := ConvertCompleteReaderResult(bookScope, SourceAudiobookshelf, bookResult); err != nil {
		t.Fatalf("audiobookshelf audiobook scope: %v", err)
	}
	if _, err := ConvertCompleteReaderResult(bookScope, SourcePlex, bookResult); !errors.Is(err, ErrInvalidCompleteReaderResult) {
		t.Fatalf("Plex must reject audiobooks: %v", err)
	}
}
