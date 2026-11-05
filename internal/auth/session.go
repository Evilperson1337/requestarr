package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"time"
)

type User struct {
	ID          string `json:"id"`
	Role        string `json:"role"`
	Email       string `json:"email"`
	DisplayName string `json:"displayName"`
	Issuer      string `json:"-"`
	Subject     string `json:"-"`
}
type Session struct {
	Token     string
	CSRF      string
	User      User
	ExpiresAt time.Time
}
type Repository interface {
	SaveTransaction(context.Context, string, string, string, string, time.Time) error
	ConsumeTransaction(context.Context, string, string, time.Time) (string, string, error)
	Login(context.Context, string, string, string, string, string, time.Time) (User, error)
	CreateSession(context.Context, string, []byte, []byte, time.Time) error
	GetSession(context.Context, []byte, time.Time) (User, []byte, time.Time, error)
	RevokeSession(context.Context, []byte, time.Time) error
}

// SetupState reports whether first-admin setup is already complete.
type SetupState interface {
	SetupComplete(context.Context) (bool, error)
}

type BootstrapRepository interface {
	ClaimAdmin(context.Context, string, string, string, []byte, time.Time) error
}

func Hash(secret, value string) []byte {
	m := hmac.New(sha256.New, []byte(secret))
	_, _ = m.Write([]byte(value))
	return m.Sum(nil)
}
func HashString(secret, value string) string { return hex.EncodeToString(Hash(secret, value)) }
func Equal(a, b string) bool                 { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }
