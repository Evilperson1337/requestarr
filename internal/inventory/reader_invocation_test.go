package inventory

import (
	"context"
	"errors"
	"testing"
	"time"

	"git.example.com/evilperson1337/Requestarr/internal/domain"
)

func TestReaderInvocationServiceRoutesAndConverts(t *testing.T) {
	t.Parallel()
	scope := invocationScope(domain.MediaMovie)
	plex := &invocationReader{result: invocationResult(scope)}
	audiobookshelf := &invocationReader{result: invocationResult(scope)}
	service := NewReaderInvocationService(plex, audiobookshelf, func(time.Time) time.Time { return time.Unix(2, 0).UTC() })

	observation, err := service.Invoke(t.Context(), scope, SourcePlex)
	if err != nil {
		t.Fatal(err)
	}
	if plex.calls != 1 || audiobookshelf.calls != 0 || len(observation.Items) != 1 {
		t.Fatalf("plex calls=%d audiobookshelf calls=%d observation=%+v", plex.calls, audiobookshelf.calls, observation)
	}
	if got := plex.requirements; got.SourceID != scope.SourceID || len(got.SelectedLibraries) != 1 || got.SelectedLibraries[0] != "library" || len(got.EnabledMediaTypes) != 1 || got.EnabledMediaTypes[0] != domain.MediaMovie {
		t.Fatalf("requirements=%+v", got)
	}
}

func TestReaderInvocationServiceRoutesAudiobookshelfOnly(t *testing.T) {
	t.Parallel()
	scope := invocationScope(domain.MediaAudiobook)
	plex := &invocationReader{result: invocationResult(scope)}
	audiobookshelf := &invocationReader{result: invocationResult(scope)}
	service := NewReaderInvocationService(plex, audiobookshelf, func(value time.Time) time.Time { return value.UTC() })

	if _, err := service.Invoke(t.Context(), scope, SourceAudiobookshelf); err != nil {
		t.Fatal(err)
	}
	if plex.calls != 0 || audiobookshelf.calls != 1 {
		t.Fatalf("plex calls=%d audiobookshelf calls=%d", plex.calls, audiobookshelf.calls)
	}
}

func TestReaderInvocationServiceRejectsUnsupportedKindBeforeReader(t *testing.T) {
	t.Parallel()
	reader := &invocationReader{result: invocationResult(invocationScope(domain.MediaMovie))}
	service := NewReaderInvocationService(reader, reader, func(value time.Time) time.Time { return value })

	_, err := service.Invoke(t.Context(), invocationScope(domain.MediaMovie), SourceJellyfin)
	if !errors.Is(err, ErrReaderInvocation) || reader.calls != 0 {
		t.Fatalf("error=%v calls=%d", err, reader.calls)
	}
}

func TestReaderInvocationServiceDiscardsReaderResultOnError(t *testing.T) {
	t.Parallel()
	scope := invocationScope(domain.MediaMovie)
	reader := &invocationReader{result: invocationResult(scope), err: errors.New("reader failure")}
	service := NewReaderInvocationService(reader, nil, func(value time.Time) time.Time { return value })

	observation, err := service.Invoke(t.Context(), scope, SourcePlex)
	if !errors.Is(err, reader.err) || !emptyObservation(observation) || reader.calls != 1 {
		t.Fatalf("observation=%+v error=%v calls=%d", observation, err, reader.calls)
	}
}

func TestReaderInvocationServiceRechecksCancellationAfterRead(t *testing.T) {
	t.Parallel()
	scope := invocationScope(domain.MediaMovie)
	ctx, cancel := context.WithCancel(t.Context())
	reader := &invocationReader{result: invocationResult(scope), afterRead: cancel}
	service := NewReaderInvocationService(reader, nil, func(value time.Time) time.Time { return value })

	observation, err := service.Invoke(ctx, scope, SourcePlex)
	if !errors.Is(err, context.Canceled) || !emptyObservation(observation) || reader.calls != 1 {
		t.Fatalf("observation=%+v error=%v calls=%d", observation, err, reader.calls)
	}
}

type invocationReader struct {
	calls        int
	requirements CompleteReaderRequirements
	result       CompleteReaderResult
	err          error
	afterRead    func()
}

func (r *invocationReader) Read(_ context.Context, requirements CompleteReaderRequirements) (CompleteReaderResult, error) {
	r.calls++
	r.requirements = requirements
	if r.afterRead != nil {
		r.afterRead()
	}
	return r.result, r.err
}

func invocationScope(mediaType domain.MediaType) ObservationScope {
	return ObservationScope{SourceID: "abcdef00-0000-4000-8000-000000000001", ConfigurationRevision: 1, SelectedLibraries: []string{"library"}, EnabledMediaTypes: []domain.MediaType{mediaType}}
}

func invocationResult(scope ObservationScope) CompleteReaderResult {
	return CompleteReaderResult{
		SourceID: scope.SourceID, CompletedAt: time.Unix(1, 0).UTC(), Complete: true,
		Coverage: []ObservationCoverage{{RemoteLibraryID: "library", MediaType: scope.EnabledMediaTypes[0], Status: CoverageVerifiedComplete}},
		Items:    []CompleteReaderItem{{RemoteLibraryID: "library", SourceItemID: "item", MediaType: scope.EnabledMediaTypes[0], Title: "Title"}},
	}
}

func emptyObservation(observation CompleteObservation) bool {
	return observation.Scope.SourceID == "" && observation.Scope.ConfigurationRevision == 0 && len(observation.Scope.SelectedLibraries) == 0 && len(observation.Scope.EnabledMediaTypes) == 0 && len(observation.Coverage) == 0 && len(observation.Items) == 0
}
