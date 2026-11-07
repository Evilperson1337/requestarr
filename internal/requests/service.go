// Package requests contains transport- and persistence-independent request use cases.
package requests

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/evilperson1337/requestarr/internal/domain"
)

var (
	ErrForbidden               = errors.New("request creation is not permitted")
	ErrInvalid                 = errors.New("invalid request")
	ErrCatalogNotFound         = errors.New("catalog item not found")
	ErrAlreadyAvailable        = errors.New("item is already available in a library")
	ErrRequestNotFound         = errors.New("request not found")
	ErrRequestClosed           = errors.New("request is closed")
	ErrCancelNeedsConfirmation = errors.New("leaving would cancel the request")
	ErrTransitionForbidden     = errors.New("request transition is not permitted")
	ErrTransitionInvalid       = errors.New("invalid request transition")
	ErrTransitionConflict      = errors.New("request version conflict")
)

type Actor struct {
	ID   string
	Role string
}

type CreateInput struct {
	Actor Actor
	// OnBehalfOf is the person the request is made for; only a role that can
	// manage requests may set it.
	OnBehalfOf string
	CatalogID  string
	Note       string
	Platforms  domain.PlatformSelection
	Selection  domain.Selection
}

type CreateResult struct {
	Request domain.Request
	Created bool
	// Merged is true when the item already had an open Request and this call
	// added to its selection; the caller then follows the Request.
	Merged bool
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
	// MergeSelection unions a selection into an open Request, follows it for the
	// user, and reports whether the selection grew. It never narrows a Request.
	MergeSelection(ctx context.Context, requestID, userID string, selection domain.Selection) (domain.Request, bool, error)
	List(context.Context, string, ListQuery) (Page, error)
	// AutoApproveTypes is the approval policy (D036): media types whose new
	// Requests start approved.
	AutoApproveTypes(context.Context) ([]domain.MediaType, error)
	// AwaitingApprovalCount counts Requests whose status is requested.
	AwaitingApprovalCount(context.Context) (int, error)
	CatalogMediaType(context.Context, string) (domain.MediaType, error)
	ValidatePlatforms(context.Context, string, domain.PlatformSelection) error
	CatalogPlatforms(context.Context, string) ([]domain.Platform, error)
	Transition(context.Context, TransitionInput) (domain.Request, bool, error)
	// UserRole reports a person's stored role; found is false for an unknown ID.
	UserRole(ctx context.Context, userID string) (role string, found bool, err error)
	Subscribe(context.Context, string, string) (bool, error)
	Unsubscribe(ctx context.Context, requestID, userID string, confirmCancel bool) (changed, cancelled bool, err error)
}

func (s *Service) Transition(ctx context.Context, in TransitionInput) (TransitionResult, error) {
	if in.Actor.ID == "" || !domain.Role(in.Actor.Role).CanManageRequests() {
		return TransitionResult{}, ErrTransitionForbidden
	}
	if in.RequestID == "" || in.ExpectedVersion < 1 || in.OperationID == "" {
		return TransitionResult{}, ErrTransitionInvalid
	}
	if !domain.CanTransition(domain.RequestRequested, in.ToStatus) {
		return TransitionResult{}, ErrTransitionInvalid
	}
	req, changed, err := s.repo.Transition(ctx, in)
	if err != nil {
		return TransitionResult{}, err
	}
	return TransitionResult{Request: req, Changed: changed}, nil
}

func (s *Service) List(ctx context.Context, actor Actor, query ListQuery) (Page, error) {
	if actor.ID == "" {
		return Page{}, ErrForbidden
	}
	query, err := query.Normalize()
	if err != nil {
		return Page{}, err
	}
	return s.repo.List(ctx, actor.ID, query)
}

func (s *Service) Subscribe(ctx context.Context, actor Actor, requestID string) (bool, error) {
	if actor.ID == "" {
		return false, ErrForbidden
	}
	if requestID == "" {
		return false, ErrInvalid
	}
	changed, err := s.repo.Subscribe(ctx, requestID, actor.ID)
	if errors.Is(err, ErrRequestNotFound) || errors.Is(err, ErrRequestClosed) {
		return false, err
	}
	if err != nil {
		return false, fmt.Errorf("subscribe to request: %w", err)
	}
	return changed, nil
}

// Unsubscribe lets any subscriber, including the creator, leave a Request. It
// reports whether the Request was cancelled because the last subscriber left;
// that needs confirmCancel, otherwise ErrCancelNeedsConfirmation and no change.
func (s *Service) Unsubscribe(ctx context.Context, actor Actor, requestID string, confirmCancel bool) (changed, cancelled bool, err error) {
	if actor.ID == "" {
		return false, false, ErrForbidden
	}
	if requestID == "" {
		return false, false, ErrInvalid
	}
	changed, cancelled, err = s.repo.Unsubscribe(ctx, requestID, actor.ID, confirmCancel)
	if errors.Is(err, ErrRequestNotFound) || errors.Is(err, ErrCancelNeedsConfirmation) {
		return false, false, err
	}
	if err != nil {
		return false, false, fmt.Errorf("unsubscribe from request: %w", err)
	}
	return changed, cancelled, nil
}

func (s *Service) CatalogPlatforms(ctx context.Context, catalogID string) ([]domain.Platform, error) {
	return s.repo.CatalogPlatforms(ctx, catalogID)
}

// autoApproves reports whether the approval policy (D036) approves this media
// type on creation. If the policy cannot be read the Request waits for a
// person, which is the safe direction.
func (s *Service) autoApproves(ctx context.Context, media domain.MediaType) bool {
	types, err := s.repo.AutoApproveTypes(ctx)
	if err != nil {
		return false
	}
	for _, t := range types {
		if t == media {
			return true
		}
	}
	return false
}

// AwaitingApprovalCount is how many Requests still have status requested; only
// an administrator can act on them, so only an administrator may ask.
func (s *Service) AwaitingApprovalCount(ctx context.Context, actor Actor) (int, error) {
	if actor.ID == "" || !domain.Role(actor.Role).CanManageRequests() {
		return 0, ErrForbidden
	}
	return s.repo.AwaitingApprovalCount(ctx)
}

type Service struct{ repo Repository }

func New(repo Repository) *Service { return &Service{repo: repo} }

func (s *Service) Create(ctx context.Context, in CreateInput) (CreateResult, error) {
	if !domain.Role(in.Actor.Role).CanRequest() {
		return CreateResult{}, ErrForbidden
	}
	if in.Actor.ID == "" || in.CatalogID == "" || len(in.Note) > 2000 {
		return CreateResult{}, ErrInvalid
	}
	// On behalf: the person becomes the requester and everything below
	// (approval policy, subscription, notifications) treats them as the asker.
	requester, madeBy := in.Actor.ID, ""
	if in.OnBehalfOf != "" {
		if !domain.Role(in.Actor.Role).CanManageRequests() {
			return CreateResult{}, ErrForbidden
		}
		role, found, err := s.repo.UserRole(ctx, in.OnBehalfOf)
		if err != nil {
			return CreateResult{}, fmt.Errorf("load requester: %w", err)
		}
		if !found || !domain.Role(role).CanRequest() {
			return CreateResult{}, fmt.Errorf("%w: the chosen person cannot make requests", ErrInvalid)
		}
		if in.OnBehalfOf != in.Actor.ID {
			requester, madeBy = in.OnBehalfOf, in.Actor.ID
		}
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
	if err := in.Selection.Validate(resolvedType); err != nil {
		return CreateResult{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	status := domain.RequestRequested
	if s.autoApproves(ctx, resolvedType) {
		status = domain.RequestApproved
	}
	req := domain.Request{CatalogItemID: in.CatalogID, Status: status, RequestedBy: requester, MadeOnBehalfBy: madeBy, Note: in.Note, Platforms: in.Platforms, Selection: in.Selection}
	sub := domain.RequestSubscription{UserID: requester, Source: domain.SubscriptionRequester}
	created, isNew, err := s.repo.CreateOrGet(ctx, req, sub)
	if err != nil {
		return CreateResult{}, fmt.Errorf("create request: %w", err)
	}
	if !isNew && created.Status.IsOpen() && !created.Selection.Contains(in.Selection) {
		merged, changed, err := s.repo.MergeSelection(ctx, created.ID, requester, in.Selection)
		if err != nil {
			return CreateResult{}, fmt.Errorf("merge request selection: %w", err)
		}
		return CreateResult{Request: merged, Merged: changed}, nil
	}
	if !isNew && in.OnBehalfOf != "" {
		// One shared request per item: the person follows the existing one.
		if _, err := s.repo.Subscribe(ctx, created.ID, requester); err != nil && !errors.Is(err, ErrRequestClosed) {
			return CreateResult{}, fmt.Errorf("subscribe requester: %w", err)
		}
	}
	return CreateResult{Request: created, Created: isNew}, nil
}
