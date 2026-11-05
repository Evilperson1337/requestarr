package app

import (
	"context"
	"errors"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/httpapi"
	"github.com/evilperson1337/requestarr/internal/store/postgres"
)

type peopleStore interface {
	DefaultFirstSignInRole(context.Context) (domain.Role, error)
	SetDefaultFirstSignInRole(context.Context, domain.Role) error
}

// peopleSettings is the people policy the HTTP layer sees: the store's
// validation error becomes the transport package's sentinel, so httpapi never
// names the adapter.
type peopleSettings struct{ store peopleStore }

func (p peopleSettings) DefaultFirstSignInRole(ctx context.Context) (domain.Role, error) {
	return p.store.DefaultFirstSignInRole(ctx)
}

func (p peopleSettings) SetDefaultFirstSignInRole(ctx context.Context, role domain.Role) error {
	err := p.store.SetDefaultFirstSignInRole(ctx, role)
	if errors.Is(err, postgres.ErrInvalidDefaultRole) {
		return httpapi.ErrInvalidDefaultRole
	}
	return err
}

type approvalStore interface {
	AutoApproveTypes(context.Context) ([]domain.MediaType, error)
	SetAutoApproveTypes(context.Context, []domain.MediaType) error
}

// approvalSettings maps the store's validation error the same way.
type approvalSettings struct{ store approvalStore }

func (a approvalSettings) AutoApproveTypes(ctx context.Context) ([]domain.MediaType, error) {
	return a.store.AutoApproveTypes(ctx)
}

func (a approvalSettings) SetAutoApproveTypes(ctx context.Context, types []domain.MediaType) error {
	err := a.store.SetAutoApproveTypes(ctx, types)
	if errors.Is(err, postgres.ErrInvalidMediaType) {
		return httpapi.ErrInvalidMediaType
	}
	return err
}
