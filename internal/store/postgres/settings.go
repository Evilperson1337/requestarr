package postgres

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/evilperson1337/requestarr/internal/domain"
)

// settingDefaultRole is the role a person gets the first time they sign in (D035).
const settingDefaultRole = "people.default_role"

// ErrInvalidDefaultRole is returned for any default first-sign-in role other
// than viewer or requester. An administrator is never granted by default.
var ErrInvalidDefaultRole = errors.New("default role must be viewer or requester")

// Setting reads one stored setting; ok is false when it was never set.
func (s *Store) Setting(ctx context.Context, key string) (value string, ok bool, err error) {
	err = s.pool.QueryRow(ctx, `SELECT value FROM app_settings WHERE key=$1`, key).Scan(&value)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read setting: %w", err)
	}
	return value, true, nil
}

// SetSetting stores one setting, replacing any earlier value.
func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	if _, err := s.pool.Exec(ctx, `INSERT INTO app_settings(key,value,updated_at) VALUES($1,$2,now()) ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value,updated_at=EXCLUDED.updated_at`, key, value); err != nil {
		return fmt.Errorf("store setting: %w", err)
	}
	return nil
}

// settingAutoApprove holds the media types whose new Requests start approved
// (D036), as a comma-separated list. Absent means none.
const settingAutoApprove = "approval.auto_types"

// ErrInvalidMediaType is returned when the approval policy names a media type
// Requestarr does not have.
var ErrInvalidMediaType = errors.New("unknown media type")

// canonicalMediaTypes is the order the policy is stored and returned in.
var canonicalMediaTypes = []domain.MediaType{domain.MediaMovie, domain.MediaTV, domain.MediaAlbum, domain.MediaSong, domain.MediaGame, domain.MediaAudiobook}

// AutoApproveTypes returns the media types whose Requests are approved on
// creation; none unless an administrator chose some. A stored name that is no
// longer a media type is ignored.
func (s *Store) AutoApproveTypes(ctx context.Context) ([]domain.MediaType, error) {
	v, _, err := s.Setting(ctx, settingAutoApprove)
	if err != nil {
		return nil, err
	}
	chosen := map[domain.MediaType]bool{}
	for _, name := range strings.Split(v, ",") {
		chosen[domain.MediaType(strings.TrimSpace(name))] = true
	}
	out := []domain.MediaType{}
	for _, t := range canonicalMediaTypes {
		if chosen[t] {
			out = append(out, t)
		}
	}
	return out, nil
}

// SetAutoApproveTypes replaces the approval policy. It applies to Requests
// created afterwards and never changes an existing Request.
func (s *Store) SetAutoApproveTypes(ctx context.Context, types []domain.MediaType) error {
	chosen := map[domain.MediaType]bool{}
	for _, t := range types {
		if !slices.Contains(canonicalMediaTypes, t) {
			return fmt.Errorf("%w: %q", ErrInvalidMediaType, t)
		}
		chosen[t] = true
	}
	names := []string{}
	for _, t := range canonicalMediaTypes {
		if chosen[t] {
			names = append(names, string(t))
		}
	}
	return s.SetSetting(ctx, settingAutoApprove, strings.Join(names, ","))
}

// firstSignInRole turns a stored value into a role; anything but requester is viewer.
func firstSignInRole(stored string) string {
	if stored == "requester" {
		return "requester"
	}
	return "viewer"
}

// DefaultFirstSignInRole is the role new people receive; viewer unless set.
func (s *Store) DefaultFirstSignInRole(ctx context.Context) (domain.Role, error) {
	v, _, err := s.Setting(ctx, settingDefaultRole)
	if err != nil {
		return "", err
	}
	return domain.Role(firstSignInRole(v)), nil
}

// SetDefaultFirstSignInRole changes the role for people who sign in from now
// on; it never changes an existing user.
func (s *Store) SetDefaultFirstSignInRole(ctx context.Context, role domain.Role) error {
	if !role.ValidDefault() {
		return ErrInvalidDefaultRole
	}
	return s.SetSetting(ctx, settingDefaultRole, string(role))
}

const (
	settingIGDBClientID       = "igdb.client_id"
	settingChecklistDismissed = "setup.checklist_dismissed"
)

// LoadIGDBClientID returns the saved IGDB client ID. A value saved in Settings
// wins, including an explicitly cleared one. An installation that predates it
// may only have the ID in the retired setup draft (D038), so that is read as a
// fallback; the read never writes.
func (s *Store) LoadIGDBClientID(ctx context.Context) (string, error) {
	if id, ok, err := s.Setting(ctx, settingIGDBClientID); err != nil || ok {
		return id, err
	}
	var id string
	err := s.pool.QueryRow(ctx, `SELECT COALESCE(draft->'providers'->>'igdbClientId','') FROM setup_state WHERE singleton AND draft_revision > 0`).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return id, err
}

// SetIGDBClientID saves the non-secret IGDB client ID; empty clears it.
func (s *Store) SetIGDBClientID(ctx context.Context, id string) error {
	return s.SetSetting(ctx, settingIGDBClientID, id)
}

// PolicyReviewed reports whether an administrator ever saved the default role
// for new people or the approval policy.
func (s *Store) PolicyReviewed(ctx context.Context) (bool, error) {
	for _, key := range []string{settingDefaultRole, settingAutoApprove} {
		if _, ok, err := s.Setting(ctx, key); err != nil || ok {
			return ok, err
		}
	}
	return false, nil
}

func (s *Store) ChecklistDismissed(ctx context.Context) (bool, error) {
	_, ok, err := s.Setting(ctx, settingChecklistDismissed)
	return ok, err
}

func (s *Store) DismissChecklist(ctx context.Context) error {
	return s.SetSetting(ctx, settingChecklistDismissed, "true")
}
