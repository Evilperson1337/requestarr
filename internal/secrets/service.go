package secrets

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

var (
	ErrNoMasterKey   = errors.New("no master key is configured")
	ErrUnknownSecret = errors.New("unknown secret")
	ErrInvalidValue  = errors.New("invalid secret value")
)

const maxValueLen = 4096

// Source says where a secret's effective value comes from (D024). The string
// values are the labels shown to administrators.
type Source string

const (
	SourceNone        Source = "none"
	SourceEnvironment Source = "environment"
	SourceStored      Source = "stored"
	SourceShadowed    Source = "stored (shadowed by environment)" // the environment value is in effect
	SourceUnreadable  Source = "stored secret unreadable"         // a row exists but cannot be opened; never treated as unconfigured
)

// Stored is one encrypted row. It never carries a plaintext value.
type Stored struct {
	Name      Name
	Sealed    // embedded: Ciphertext, KeyID
	UpdatedAt time.Time
}

// Repository persists encrypted rows. Put and Delete record a value-free audit
// event in the same transaction.
type Repository interface {
	ListStoredSecrets(ctx context.Context) ([]Stored, error)
	PutStoredSecret(ctx context.Context, actorID string, name Name, sealed Sealed) error
	DeleteStoredSecret(ctx context.Context, actorID string, name Name) error
}

// Resolved is a secret's effective value. Value must never be logged or returned
// over HTTP.
type Resolved struct {
	Value  string
	Source Source
	// fingerprint identifies the value, so pending-restart can be detected
	// without keeping a second copy of it.
	fingerprint string
}

// Resolved and Service hold plaintext, so formatting or logging them prints a
// placeholder instead of the values.
func (r Resolved) String() string {
	return "secrets.Resolved{source:" + string(r.Source) + ", value:[redacted]}"
}
func (r Resolved) GoString() string     { return r.String() }
func (r Resolved) LogValue() slog.Value { return slog.StringValue(r.String()) }
func (s *Service) String() string       { return "secrets.Service{[redacted]}" }
func (s *Service) GoString() string     { return s.String() }
func (s *Service) LogValue() slog.Value { return slog.StringValue(s.String()) }

// Status is the write-only view of a secret: presence and source, never the value.
type Status struct {
	Name           Name
	EnvVar         string
	Label          string
	Configured     bool
	Source         Source
	Unreadable     bool
	PendingRestart bool
}

// Service resolves and manages stored secrets. sealer is nil without a master key.
type Service struct {
	repo   Repository
	sealer Sealer
	env    map[Name]string

	mu   sync.Mutex
	boot map[Name]string // fingerprints at startup; nil until Boot
}

// NewService takes the environment values as captured at startup. sealer is nil
// when no master key is configured.
func NewService(repo Repository, sealer Sealer, env map[Name]string) *Service {
	return &Service{repo: repo, sealer: sealer, env: env}
}

// Enabled reports whether secret values can be accepted.
func (s *Service) Enabled() bool { return s.sealer != nil }

func (s *Service) resolve(name Name, row *Stored) Resolved {
	envValue := s.env[name]
	switch {
	case envValue != "" && row != nil:
		return Resolved{Value: envValue, Source: SourceShadowed, fingerprint: "env"}
	case envValue != "":
		return Resolved{Value: envValue, Source: SourceEnvironment, fingerprint: "env"}
	case row == nil:
		return Resolved{Source: SourceNone, fingerprint: "none"}
	case s.sealer == nil:
		return Resolved{Source: SourceUnreadable, fingerprint: "none"}
	}
	plain, err := s.sealer.Open(name, row.Sealed)
	if err != nil {
		return Resolved{Source: SourceUnreadable, fingerprint: "none"}
	}
	return Resolved{Value: string(plain), Source: SourceStored, fingerprint: "stored@" + strconv.FormatInt(row.UpdatedAt.UnixMicro(), 10)}
}

func (s *Service) rows(ctx context.Context) (map[Name]*Stored, error) {
	list, err := s.repo.ListStoredSecrets(ctx)
	if err != nil {
		return nil, fmt.Errorf("list stored secrets: %w", err)
	}
	out := make(map[Name]*Stored, len(list))
	for i := range list {
		out[list[i].Name] = &list[i]
	}
	return out, nil
}

// Resolve returns the value the next restart would use for name.
func (s *Service) Resolve(ctx context.Context, name Name) (Resolved, error) {
	if _, ok := Lookup(name); !ok {
		return Resolved{}, ErrUnknownSecret
	}
	rows, err := s.rows(ctx)
	if err != nil {
		return Resolved{}, err
	}
	return s.resolve(name, rows[name]), nil
}

// Boot resolves every registered secret (environment, then stored) and records
// what is in effect, so later calls can report "saved, pending restart". An
// unreadable secret yields no value but does not fail the boot.
func (s *Service) Boot(ctx context.Context) (map[Name]string, error) {
	rows, err := s.rows(ctx)
	if err != nil {
		return nil, err
	}
	values := map[Name]string{}
	boot := map[Name]string{}
	for _, spec := range All() {
		r := s.resolve(spec.Name, rows[spec.Name])
		values[spec.Name] = r.Value
		boot[spec.Name] = r.fingerprint
	}
	s.mu.Lock()
	s.boot = boot
	s.mu.Unlock()
	return values, nil
}

// Status lists every registered secret's source and presence, and whether any
// secret changes will only apply after a restart.
func (s *Service) Status(ctx context.Context) ([]Status, bool, error) {
	rows, err := s.rows(ctx)
	if err != nil {
		return nil, false, err
	}
	s.mu.Lock()
	boot := s.boot
	s.mu.Unlock()
	var out []Status
	for _, spec := range All() {
		r := s.resolve(spec.Name, rows[spec.Name])
		st := Status{Name: spec.Name, EnvVar: spec.EnvVar, Label: spec.Label, Source: r.Source,
			Configured: r.Value != "", Unreadable: r.Source == SourceUnreadable}
		if boot != nil {
			st.PendingRestart = boot[spec.Name] != r.fingerprint
		}
		out = append(out, st)
	}
	return out, s.Enabled(), nil
}

func validValue(v string) bool {
	if v == "" || len(v) > maxValueLen || strings.TrimSpace(v) != v {
		return false
	}
	for _, r := range v {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// Set encrypts and stores value. The value is never returned, logged or audited.
func (s *Service) Set(ctx context.Context, actorID string, name Name, value string) error {
	if _, ok := Lookup(name); !ok {
		return ErrUnknownSecret
	}
	if s.sealer == nil {
		return ErrNoMasterKey
	}
	if !validValue(value) {
		return ErrInvalidValue
	}
	sealed, err := s.sealer.Seal(name, []byte(value))
	if err != nil {
		return fmt.Errorf("seal secret: %w", err)
	}
	return s.repo.PutStoredSecret(ctx, actorID, name, sealed)
}

// Clear deletes a stored secret. It works without a master key, so an operator
// can discard a secret they can no longer read.
func (s *Service) Clear(ctx context.Context, actorID string, name Name) error {
	if _, ok := Lookup(name); !ok {
		return ErrUnknownSecret
	}
	return s.repo.DeleteStoredSecret(ctx, actorID, name)
}
