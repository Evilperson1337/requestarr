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
	// ErrNoWorkingSignIn: another admin exists, but nobody can sign in as them
	// because OIDC is off or its provider is not working.
	ErrNoWorkingSignIn = errors.New("turn on OIDC and make sure another admin can sign in first")
	ErrBuiltinRole     = errors.New("the local administrator always has the superuser role")
	ErrAdminManagement = errors.New("could not update the local administrator")
)

type LocalAdminStatus struct {
	Exists   bool `json:"exists"`
	Disabled bool `json:"disabled"`
	// CanDisable is true only while another admin exists AND OIDC is working,
	// so someone can really sign in once the local administrator is off.
	CanDisable bool `json:"canDisable"`
	// OtherAdmin and OIDCWorking are the two halves of CanDisable, so the page
	// can say which one is missing.
	OtherAdmin  bool `json:"otherAdminExists"`
	OIDCWorking bool `json:"oidcWorking"`
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
	// SetLocalAdminDisabled disables or enables the account. When disabling, the
	// repository checks that another admin exists and then calls guard (if not
	// nil) inside the same serialised section; a false guard returns
	// ErrNoWorkingSignIn.
	SetLocalAdminDisabled(ctx context.Context, actor string, disabled bool, now time.Time, guard func() bool) error
}

// AdminManagement is the service seam for Authentication settings, not a
// general local-account manager. Only the one built-in account can be created.
type AdminManagement struct {
	Repo   AdminManagementRepository
	Hasher PasswordHasher
	// OIDCWorking reports, from the live OIDC state, whether OIDC is enabled
	// and its provider is active. Nil means it is not (the safe answer).
	OIDCWorking func() bool
}

func (s AdminManagement) oidcWorking() bool { return s.OIDCWorking != nil && s.OIDCWorking() }

func (s AdminManagement) LocalAdminStatus(ctx context.Context) (LocalAdminStatus, error) {
	st, err := s.Repo.LocalAdminStatus(ctx)
	if err != nil {
		return LocalAdminStatus{}, ErrAdminManagement
	}
	st.OIDCWorking = s.oidcWorking()
	st.CanDisable = st.CanDisable && st.OIDCWorking
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
	err := s.Repo.SetLocalAdminDisabled(ctx, actor, disabled, time.Now().UTC(), s.oidcWorking)
	if err == nil || errors.Is(err, ErrLastLocalAdmin) || errors.Is(err, ErrNoWorkingSignIn) || errors.Is(err, ErrNoCredential) {
		return err
	}
	return ErrAdminManagement
}
