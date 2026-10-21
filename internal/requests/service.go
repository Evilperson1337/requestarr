// Package requests contains transport- and persistence-independent request use cases.
package requests

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/example/requestarr/internal/domain"
)

var (
	ErrForbidden             = errors.New("request creation is not permitted")
	ErrInvalid               = errors.New("invalid request")
	ErrCatalogNotFound       = errors.New("catalog item not found")
	ErrRequestNotFound       = errors.New("request not found")
	ErrRequesterSubscription = errors.New("creator subscription cannot be removed")
	ErrTransitionForbidden   = errors.New("request transition is not permitted")
	ErrTransitionInvalid     = errors.New("invalid request transition")
	ErrTransitionConflict    = errors.New("request version conflict")
)

type Actor struct {
	ID   string
	Role string
}

type CreateInput struct {
	Actor     Actor
	CatalogID string
	Note      string
	Platforms domain.PlatformSelection
}

type CreateResult struct {
	Request domain.Request
	Created bool
}

type TransitionInput struct {
	Actor           Actor
	RequestID       string
	ToStatus        domain.RequestStatus
	ExpectedVersion int64
	OperationID     string
}

type TransitionResult struct {
	Request domain.Request
	Changed bool
}

// Repository atomically creates a request and its creator subscription, or
// returns the existing request without subscribing the losing caller.
type Repository interface {
	CreateOrGet(context.Context, domain.Request, domain.RequestSubscription) (domain.Request, bool, error)
	List(context.Context, string, bool) ([]domain.Request, error)
	CatalogMediaType(context.Context, string) (domain.MediaType, error)
	ValidatePlatforms(context.Context, string, domain.PlatformSelection) error
	CatalogPlatforms(context.Context, string) ([]domain.Platform, error)
	Transition(context.Context, TransitionInput) (domain.Request, bool, error)
	Subscribe(context.Context, string, string) (bool, error)
	Unsubscribe(context.Context, string, string) (bool, error)
}

func (s *Service) Transition(ctx context.Context, in TransitionInput) (TransitionResult, error) {
	if in.Actor.ID == "" || in.Actor.Role != "admin" {
		return TransitionResult{}, ErrTransitionForbidden
	}
	if in.RequestID == "" || in.ExpectedVersion < 1 || in.OperationID == "" {
		return TransitionResult{}, ErrTransitionInvalid
	}
	if in.ToStatus != domain.RequestApproved && in.ToStatus != domain.RequestDenied {
		return TransitionResult{}, ErrTransitionInvalid
	}
	req, changed, err := s.repo.Transition(ctx, in)
	if err != nil {
		return TransitionResult{}, err
	}
	return TransitionResult{Request: req, Changed: changed}, nil
}

func (s *Service) List(ctx context.Context, actor Actor, mine bool) ([]domain.Request, error) {
	if actor.ID == "" {
		return nil, ErrForbidden
	}
	return s.repo.List(ctx, actor.ID, mine)
}

func (s *Service) Subscribe(ctx context.Context, actor Actor, requestID string) (bool, error) {
	if actor.ID == "" {
		return false, ErrForbidden
	}
	if requestID == "" {
		return false, ErrInvalid
	}
	changed, err := s.repo.Subscribe(ctx, requestID, actor.ID)
	if errors.Is(err, ErrRequestNotFound) {
		return false, err
	}
	if err != nil {
		return false, fmt.Errorf("subscribe to request: %w", err)
	}
	return changed, nil
}

func (s *Service) Unsubscribe(ctx context.Context, actor Actor, requestID string) (bool, error) {
	if actor.ID == "" {
		return false, ErrForbidden
	}
	if requestID == "" {
		return false, ErrInvalid
	}
	changed, err := s.repo.Unsubscribe(ctx, requestID, actor.ID)
	if errors.Is(err, ErrRequestNotFound) || errors.Is(err, ErrRequesterSubscription) {
		return false, err
	}
	if err != nil {
		return false, fmt.Errorf("unsubscribe from request: %w", err)
	}
	return changed, nil
}

func (s *Service) CatalogPlatforms(ctx context.Context, catalogID string) ([]domain.Platform, error) {
	return s.repo.CatalogPlatforms(ctx, catalogID)
}

type Service struct{ repo Repository }

func New(repo Repository) *Service { return &Service{repo: repo} }

func (s *Service) Create(ctx context.Context, in CreateInput) (CreateResult, error) {
	if in.Actor.Role != "requester" && in.Actor.Role != "admin" {
		return CreateResult{}, ErrForbidden
	}
	if in.Actor.ID == "" || in.CatalogID == "" || len(in.Note) > 2000 {
		return CreateResult{}, ErrInvalid
	}
	if strings.TrimSpace(in.Note) == "" {
		in.Note = ""
	}
	resolvedType, err := s.repo.CatalogMediaType(ctx, in.CatalogID)
	if errors.Is(err, ErrCatalogNotFound) {
		return CreateResult{}, ErrCatalogNotFound
	}
	if err != nil {
		return CreateResult{}, fmt.Errorf("load catalog item: %w", err)
	}
	if resolvedType != domain.MediaGame {
		in.Platforms = domain.PlatformSelection{}
	} else if !domain.ValidatePlatformSelection(in.Platforms) {
		return CreateResult{}, fmt.Errorf("%w: game requires Any or registered platforms", ErrInvalid)
	}
	if err := s.repo.ValidatePlatforms(ctx, in.CatalogID, in.Platforms); err != nil {
		return CreateResult{}, fmt.Errorf("%w: validate platform selection: %v", ErrInvalid, err)
	}
	req := domain.Request{CatalogItemID: in.CatalogID, Status: domain.RequestRequested, RequestedBy: in.Actor.ID, Note: in.Note, Platforms: in.Platforms}
	sub := domain.RequestSubscription{UserID: in.Actor.ID, Source: domain.SubscriptionRequester}
	created, isNew, err := s.repo.CreateOrGet(ctx, req, sub)
	if err != nil {
		return CreateResult{}, fmt.Errorf("create request: %w", err)
	}
	return CreateResult{Request: created, Created: isNew}, nil
}
