package auth

import (
	"context"
	"errors"
	"github.com/evilperson1337/requestarr/internal/auth/password"
	"time"
)

var (
	ErrLocalAdminExists = errors.New("the local administrator already exists; use password recovery instead")
	ErrLastLocalAdmin   = errors.New("cannot disable the last enabled administrator")
	ErrBuiltinRole      = errors.New("the local administrator always has the admin role")
	ErrAdminManagement  = errors.New("could not update the local administrator")
)

type LocalAdminStatus struct {
	Exists     bool `json:"exists"`
	Disabled   bool `json:"disabled"`
	CanDisable bool `json:"canDisable"`
}
type CreatedLocalAdmin struct {
	Username           string `json:"username"`
	Password           string `json:"password"`
	MustChangePassword bool   `json:"mustChangePassword"`
}

func (CreatedLocalAdmin) String() string     { return "local administrator credential [redacted]" }
func (v CreatedLocalAdmin) GoString() string { return v.String() }

type AdminManagementRepository interface {
	LocalAdminStatus(context.Context) (LocalAdminStatus, error)
	CreateRecoveryAdmin(context.Context, string, string, time.Time) (bool, error)
	SetLocalAdminDisabled(context.Context, string, bool, time.Time) error
}

// AdminManagement is the service seam for Authentication settings, not a
// general local-account manager. Only the one built-in account can be created.
type AdminManagement struct {
	Repo   AdminManagementRepository
	Hasher PasswordHasher
}

func (s AdminManagement) LocalAdminStatus(ctx context.Context) (LocalAdminStatus, error) {
	st, err := s.Repo.LocalAdminStatus(ctx)
	if err != nil {
		return LocalAdminStatus{}, ErrAdminManagement
	}
	return st, nil
}
func (s AdminManagement) Create(ctx context.Context, actor string) (CreatedLocalAdmin, error) {
	pw, err := password.Random()
	if err != nil {
		return CreatedLocalAdmin{}, ErrAdminManagement
	}
	h := s.Hasher
	if h == nil {
		h = password.Default
	}
	hash, err := h.Hash(pw)
	if err != nil {
		return CreatedLocalAdmin{}, ErrAdminManagement
	}
	created, err := s.Repo.CreateRecoveryAdmin(ctx, actor, hash, time.Now().UTC())
	if err != nil {
		return CreatedLocalAdmin{}, ErrAdminManagement
	}
	if !created {
		return CreatedLocalAdmin{}, ErrLocalAdminExists
	}
	return CreatedLocalAdmin{Username: LocalUsername, Password: pw, MustChangePassword: true}, nil
}
func (s AdminManagement) SetDisabled(ctx context.Context, actor string, disabled bool) error {
	err := s.Repo.SetLocalAdminDisabled(ctx, actor, disabled, time.Now().UTC())
	if err == nil || errors.Is(err, ErrLastLocalAdmin) || errors.Is(err, ErrNoCredential) {
		return err
	}
	return ErrAdminManagement
}
