package app

import (
	"context"
	"errors"
	"testing"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/httpapi"
	"github.com/evilperson1337/requestarr/internal/store/postgres"
)

type failingPolicyStore struct{ err error }

func (f failingPolicyStore) DefaultFirstSignInRole(context.Context) (domain.Role, error) {
	return domain.RoleViewer, nil
}
func (f failingPolicyStore) SetDefaultFirstSignInRole(context.Context, domain.Role) error {
	return f.err
}
func (f failingPolicyStore) AutoApproveTypes(context.Context) ([]domain.MediaType, error) {
	return nil, nil
}
func (f failingPolicyStore) SetAutoApproveTypes(context.Context, []domain.MediaType) error {
	return f.err
}

func TestStoreValidationErrorsBecomeTransportSentinels(t *testing.T) {
	ctx := context.Background()
	people := peopleSettings{failingPolicyStore{postgres.ErrInvalidDefaultRole}}
	if err := people.SetDefaultFirstSignInRole(ctx, "admin"); !errors.Is(err, httpapi.ErrInvalidDefaultRole) {
		t.Errorf("people error = %v, want httpapi.ErrInvalidDefaultRole", err)
	}
	approvals := approvalSettings{failingPolicyStore{postgres.ErrInvalidMediaType}}
	if err := approvals.SetAutoApproveTypes(ctx, []domain.MediaType{"podcast"}); !errors.Is(err, httpapi.ErrInvalidMediaType) {
		t.Errorf("approvals error = %v, want httpapi.ErrInvalidMediaType", err)
	}
	other := errors.New("db down")
	if err := (peopleSettings{failingPolicyStore{other}}).SetDefaultFirstSignInRole(ctx, "viewer"); !errors.Is(err, other) || errors.Is(err, httpapi.ErrInvalidDefaultRole) {
		t.Errorf("other error = %v, want it passed through", err)
	}
}
