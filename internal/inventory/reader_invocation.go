package inventory

import (
	"context"
	"errors"
	"fmt"
	"time"

	"git.example.com/evilperson1337/Requestarr/internal/domain"
)

var ErrReaderInvocation = errors.New("inventory reader invocation failed")

// CompleteReader reads one fully configured source scope without persisting or
// otherwise acting on the result.
type CompleteReader interface {
	Read(context.Context, CompleteReaderRequirements) (CompleteReaderResult, error)
}

// ReaderInvocationService invokes exactly one supported source reader and
// converts its result to a complete observation. It has no configuration,
// scheduling, transport, or persistence dependencies.
type ReaderInvocationService struct {
	plex             CompleteReader
	audiobookshelf   CompleteReader
	canonicalizeTime func(time.Time) time.Time
}

// NewReaderInvocationService constructs a read-only invocation service. The
// supplied time policy must be deterministic and must not read the wall clock.
func NewReaderInvocationService(plex, audiobookshelf CompleteReader, canonicalizeTime func(time.Time) time.Time) *ReaderInvocationService {
	return &ReaderInvocationService{plex: plex, audiobookshelf: audiobookshelf, canonicalizeTime: canonicalizeTime}
}

// Invoke validates and copies the trusted scope, derives reader requirements
// exclusively from it, and invokes the matching reader once.
func (s *ReaderInvocationService) Invoke(ctx context.Context, scope ObservationScope, kind SourceKind) (CompleteObservation, error) {
	if ctx == nil {
		return CompleteObservation{}, fmt.Errorf("%w: context is required", ErrReaderInvocation)
	}
	if err := ctx.Err(); err != nil {
		return CompleteObservation{}, fmt.Errorf("%w: %w", ErrReaderInvocation, err)
	}
	if err := validateObservationScope(scope); err != nil {
		return CompleteObservation{}, fmt.Errorf("%w: invalid trusted scope: %v", ErrReaderInvocation, err)
	}
	if s == nil || s.canonicalizeTime == nil {
		return CompleteObservation{}, fmt.Errorf("%w: deterministic completion-time policy is required", ErrReaderInvocation)
	}

	trustedScope := cloneObservationScope(scope)
	requirements := CompleteReaderRequirements{
		SourceID:          trustedScope.SourceID,
		SelectedLibraries: append([]string(nil), trustedScope.SelectedLibraries...),
		EnabledMediaTypes: append([]domain.MediaType(nil), trustedScope.EnabledMediaTypes...),
	}
	reader, err := s.reader(kind)
	if err != nil {
		return CompleteObservation{}, err
	}
	result, err := reader.Read(ctx, requirements)
	if err != nil {
		return CompleteObservation{}, err
	}
	if err := ctx.Err(); err != nil {
		return CompleteObservation{}, fmt.Errorf("%w: %w", ErrReaderInvocation, err)
	}
	result.CompletedAt = s.canonicalizeTime(result.CompletedAt)
	if err := ctx.Err(); err != nil {
		return CompleteObservation{}, fmt.Errorf("%w: %w", ErrReaderInvocation, err)
	}
	return ConvertCompleteReaderResult(trustedScope, kind, result)
}

func (s *ReaderInvocationService) reader(kind SourceKind) (CompleteReader, error) {
	if s == nil {
		return nil, fmt.Errorf("%w: service is required", ErrReaderInvocation)
	}
	switch kind {
	case SourcePlex:
		if s.plex != nil {
			return s.plex, nil
		}
	case SourceAudiobookshelf:
		if s.audiobookshelf != nil {
			return s.audiobookshelf, nil
		}
	default:
		return nil, fmt.Errorf("%w: unsupported source kind", ErrReaderInvocation)
	}
	return nil, fmt.Errorf("%w: reader is required", ErrReaderInvocation)
}
