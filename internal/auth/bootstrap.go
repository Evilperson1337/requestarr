package auth

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/evilperson1337/requestarr/internal/auth/password"
)

// AdminPasswordEnv names the variable that may supply the initial password.
const AdminPasswordEnv = "REQUESTARR_ADMIN_PASSWORD"

// BootstrapStore is what first-run creation needs.
type BootstrapStore interface {
	CreateLocalAdmin(ctx context.Context, hash string, now time.Time) (bool, error)
}

// BootstrapOptions configures BootstrapLocalAdmin.
type BootstrapOptions struct {
	PublicURL string
	// InitialPassword is REQUESTARR_ADMIN_PASSWORD; empty means generate one.
	InitialPassword string
	Hasher          PasswordHasher // defaults to password.Default
	Logger          *slog.Logger
	Now             func() time.Time
}

// ValidateInitialPassword checks REQUESTARR_ADMIN_PASSWORD. The error names the
// variable and never repeats the value.
func ValidateInitialPassword(pw string) error {
	if pw == "" {
		return nil
	}
	if !password.ValidLength(pw) {
		return fmt.Errorf("%s must be %d to %d characters", AdminPasswordEnv, MinPasswordLength, MaxPasswordLength)
	}
	return nil
}

// BootstrapLocalAdmin creates the built-in local administrator at startup, but
// only on a fresh install with no users (D040). The account must change its
// password at first sign-in whichever way the password was chosen.
//
// A generated password appears in exactly one place: the single Info line
// logged here, after the account is committed. It is never logged for an
// operator-supplied password, never returned, and never stored in plaintext.
func BootstrapLocalAdmin(ctx context.Context, store BootstrapStore, o BootstrapOptions) error {
	if err := ValidateInitialPassword(o.InitialPassword); err != nil {
		return err
	}
	hasher, now := o.Hasher, o.Now
	if hasher == nil {
		hasher = password.Default
	}
	if now == nil {
		now = time.Now
	}
	pw, generated := o.InitialPassword, false
	if pw == "" {
		var err error
		if pw, err = password.Random(); err != nil {
			return fmt.Errorf("generate the local administrator password: %w", err)
		}
		generated = true
	}
	hash, err := hasher.Hash(pw)
	if err != nil {
		return fmt.Errorf("hash the local administrator password: %w", err)
	}
	created, err := store.CreateLocalAdmin(ctx, hash, now().UTC())
	if err != nil {
		return fmt.Errorf("create the local administrator: %w", err)
	}
	if !created || o.Logger == nil {
		return nil
	}
	loginURL := strings.TrimRight(o.PublicURL, "/") + "/login"
	if generated {
		o.Logger.Info("Requestarr created the local administrator. Sign in as 'admin' at "+loginURL+" with the one-time password in this line; you will be asked to choose a new password. Anyone who can read this log can read it.",
			"username", LocalUsername, "one_time_password", pw)
		return nil
	}
	o.Logger.Info("Requestarr created the local administrator with the password from " + AdminPasswordEnv + ". Sign in as 'admin' at " + loginURL + "; you will be asked to choose a new password.")
	return nil
}
