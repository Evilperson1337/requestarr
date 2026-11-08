package auth

import (
	"context"
	"encoding/base64"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

// fakeDiagTokenStore holds tokens in memory. It stands in for PostgreSQL only
// to exercise the service's own rules; the storage rules (limit, audit rows,
// who can sign in) are proved against PostgreSQL in internal/store/postgres.
type fakeDiagTokenStore struct {
	rows      map[string]*fakeDiagRow
	audits    []AuditEvent
	creatorOK bool
}

type fakeDiagRow struct {
	tok  DiagnosticsToken
	hash string
}

func newFakeDiagTokenStore() *fakeDiagTokenStore {
	return &fakeDiagTokenStore{rows: map[string]*fakeDiagRow{}, creatorOK: true}
}

func (f *fakeDiagTokenStore) CreateDiagnosticsToken(_ context.Context, t DiagnosticsToken, hash []byte, a AuditEvent) error {
	f.rows[t.ID] = &fakeDiagRow{tok: t, hash: string(hash)}
	f.audits = append(f.audits, a)
	return nil
}
func (f *fakeDiagTokenStore) ListDiagnosticsTokens(context.Context) ([]DiagnosticsToken, error) {
	var out []DiagnosticsToken
	for _, r := range f.rows {
		out = append(out, r.tok)
	}
	return out, nil
}
func (f *fakeDiagTokenStore) RevokeDiagnosticsToken(_ context.Context, id string, now time.Time, a AuditEvent) error {
	r := f.rows[id]
	if r == nil || r.tok.RevokedAt != nil {
		return ErrDiagnosticsTokenNotFound
	}
	r.tok.RevokedAt = &now
	f.audits = append(f.audits, a)
	return nil
}
func (f *fakeDiagTokenStore) DiagnosticsTokenByHash(_ context.Context, hash []byte) (DiagnosticsTokenRecord, error) {
	for _, r := range f.rows {
		if r.hash == string(hash) {
			return DiagnosticsTokenRecord{DiagnosticsToken: r.tok, CreatorCanSignIn: f.creatorOK}, nil
		}
	}
	return DiagnosticsTokenRecord{}, ErrDiagnosticsTokenNotFound
}
func (f *fakeDiagTokenStore) TouchDiagnosticsToken(context.Context, string, time.Time) error {
	return nil
}

var superuser = User{ID: adminUserID, Role: "superuser"}

func newDiagService(store *fakeDiagTokenStore) (*DiagnosticsTokens, *fakeClock) {
	clock := &fakeClock{t: time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)}
	return &DiagnosticsTokens{Store: store, Secret: testSecret, Clock: clock.Now}, clock
}

func TestACreatedTokenIsRqdPlus32RandomBytesAndOnlyItsKeyedHashIsStored(t *testing.T) {
	store := newFakeDiagTokenStore()
	svc, _ := newDiagService(store)
	value, tok, err := svc.Create(context.Background(), superuser, "claude", 0, "198.51.100.7")
	if err != nil {
		t.Fatal(err)
	}
	raw, ok := strings.CutPrefix(value, "rqd_")
	if !ok {
		t.Fatalf("token lacks the rqd_ prefix: %q", value[:4])
	}
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(b) != 32 {
		t.Fatalf("token body is not 32 base64url bytes (err %v, len %d)", err, len(b))
	}
	row := store.rows[tok.ID]
	if row.hash != string(Hash(testSecret, value)) {
		t.Fatal("stored hash is not the session-style keyed hash of the value")
	}
	if strings.Contains(row.hash, value) || strings.Contains(row.hash, raw) {
		t.Fatal("the value is stored")
	}
	other, _, _ := svc.Create(context.Background(), superuser, "second", 0, "")
	if other == value {
		t.Fatal("two tokens are equal")
	}
}

func TestListNeverCarriesTheValueOrTheHash(t *testing.T) {
	store := newFakeDiagTokenStore()
	svc, _ := newDiagService(store)
	value, _, _ := svc.Create(context.Background(), superuser, "claude", 0, "")
	list, err := svc.List(context.Background(), superuser)
	if err != nil || len(list) != 1 {
		t.Fatalf("list %v %v", list, err)
	}
	// Whatever a list row can hold, none of it is the secret.
	for _, f := range reflect.VisibleFields(reflect.TypeOf(list[0])) {
		v := reflect.ValueOf(list[0]).FieldByIndex(f.Index)
		if s, ok := v.Interface().(string); ok && strings.Contains(s, value[4:]) {
			t.Fatalf("field %s holds the token", f.Name)
		}
		if f.Type.Kind() == reflect.Slice && f.Type.Elem().Kind() == reflect.Uint8 {
			t.Fatalf("field %s is a byte slice, which could be the hash", f.Name)
		}
	}
}

func TestExpiryDefaultsToThirtyDaysIsRefusedAboveNinetyAndMustBePositive(t *testing.T) {
	store := newFakeDiagTokenStore()
	svc, clock := newDiagService(store)
	ctx := context.Background()
	_, tok, err := svc.Create(ctx, superuser, "default", 0, "")
	if err != nil || !tok.ExpiresAt.Equal(clock.Now().Add(30*24*time.Hour)) {
		t.Fatalf("default expiry %v (%v), want 30 days", tok.ExpiresAt, err)
	}
	if _, tok, err = svc.Create(ctx, superuser, "max", 90*24*time.Hour, ""); err != nil || !tok.ExpiresAt.Equal(clock.Now().Add(90*24*time.Hour)) {
		t.Fatalf("90 days refused: %v", err)
	}
	for _, d := range []time.Duration{90*24*time.Hour + time.Second, 91 * 24 * time.Hour, -time.Hour} {
		if _, _, err := svc.Create(ctx, superuser, "bad", d, ""); !errors.Is(err, ErrDiagnosticsTokenLifetime) {
			t.Errorf("lifetime %v: err %v, want ErrDiagnosticsTokenLifetime", d, err)
		}
	}
}

func TestANameIsOneToEightyPlainCharacters(t *testing.T) {
	svc, _ := newDiagService(newFakeDiagTokenStore())
	for _, name := range []string{"", "   ", strings.Repeat("a", 81), "line\nbreak", "tab\there", "nul\x00"} {
		if _, _, err := svc.Create(context.Background(), superuser, name, 0, ""); !errors.Is(err, ErrDiagnosticsTokenName) {
			t.Errorf("name %q: err %v, want ErrDiagnosticsTokenName", name, err)
		}
	}
	if _, tok, err := svc.Create(context.Background(), superuser, "  "+strings.Repeat("é", 80)+" ", 0, ""); err != nil || tok.Name != strings.Repeat("é", 80) {
		t.Fatalf("an 80-character name was refused or not trimmed: %v", err)
	}
}

func TestOnlyASuperuserManagesTokens(t *testing.T) {
	store := newFakeDiagTokenStore()
	svc, _ := newDiagService(store)
	for _, role := range []string{"admin", "requester", "viewer", ""} {
		u := User{ID: adminUserID, Role: role}
		if _, _, err := svc.Create(context.Background(), u, "x", 0, ""); !errors.Is(err, ErrDiagnosticsTokenForbidden) {
			t.Errorf("%q create: %v", role, err)
		}
		if _, err := svc.List(context.Background(), u); !errors.Is(err, ErrDiagnosticsTokenForbidden) {
			t.Errorf("%q list: %v", role, err)
		}
		if err := svc.Revoke(context.Background(), u, "id", ""); !errors.Is(err, ErrDiagnosticsTokenForbidden) {
			t.Errorf("%q revoke: %v", role, err)
		}
	}
	if len(store.rows) != 0 {
		t.Fatal("a token was stored")
	}
}

func TestCreateAndRevokeWriteValueFreeAuditRowsWithTheActor(t *testing.T) {
	store := newFakeDiagTokenStore()
	svc, _ := newDiagService(store)
	ctx := context.Background()
	value, tok, _ := svc.Create(ctx, superuser, "claude", 0, "198.51.100.7")
	if err := svc.Revoke(ctx, superuser, tok.ID, "198.51.100.7"); err != nil {
		t.Fatal(err)
	}
	if len(store.audits) != 2 || store.audits[0].Type != AuditDiagnosticsTokenCreated || store.audits[1].Type != AuditDiagnosticsTokenRevoked {
		t.Fatalf("audits %+v", store.audits)
	}
	for _, a := range store.audits {
		if a.UserID != superuser.ID || a.RemoteAddr != "198.51.100.7" {
			t.Errorf("audit row lacks the actor or address: %+v", a)
		}
		if strings.Contains(strings.Join([]string{a.Type, a.UserID, a.Username, a.RemoteAddr}, " "), value[4:]) {
			t.Error("an audit row holds the token")
		}
	}
	if err := svc.Revoke(ctx, superuser, tok.ID, ""); !errors.Is(err, ErrDiagnosticsTokenNotFound) {
		t.Fatalf("a second revoke: %v", err)
	}
	if len(store.audits) != 2 {
		t.Fatal("a refused revoke wrote an audit row")
	}
}

func reason(err error) DiagnosticsRefusalReason {
	var r *DiagnosticsRefusal
	if errors.As(err, &r) {
		return r.Reason
	}
	return ""
}

func TestLookupAcceptsOnlyAnUnexpiredUnrevokedTokenOfACreatorWhoCanSignIn(t *testing.T) {
	store := newFakeDiagTokenStore()
	svc, clock := newDiagService(store)
	ctx := context.Background()
	value, tok, _ := svc.Create(ctx, superuser, "claude", 24*time.Hour, "")

	got, err := svc.Lookup(ctx, value)
	if err != nil || got.ID != tok.ID {
		t.Fatalf("a valid token was refused: %v", err)
	}

	for _, bad := range []string{"", "rqd_", "rqd_" + strings.Repeat("A", 43), "xyz_" + value[4:], value + "x", strings.ToUpper(value)} {
		if _, err := svc.Lookup(ctx, bad); reason(err) != DiagnosticsUnknown || !errors.Is(err, ErrDiagnosticsTokenRefused) {
			t.Errorf("%.8q: err %v", bad, err)
		}
	}

	store.creatorOK = false
	if _, err := svc.Lookup(ctx, value); reason(err) != DiagnosticsCreatorNotSuperuser {
		t.Fatalf("creator who cannot sign in: %v", err)
	}
	store.creatorOK = true

	clock.Advance(24*time.Hour + time.Second)
	if _, err := svc.Lookup(ctx, value); reason(err) != DiagnosticsExpired {
		t.Fatalf("expired: %v", err)
	}
	clock.Advance(-48 * time.Hour)
	if err := svc.Revoke(ctx, superuser, tok.ID, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Lookup(ctx, value); reason(err) != DiagnosticsRevoked {
		t.Fatalf("revoked: %v", err)
	}
}

func TestEveryRefusalReadsTheSameToACaller(t *testing.T) {
	svc, _ := newDiagService(newFakeDiagTokenStore())
	_, err := svc.Lookup(context.Background(), "rqd_nope")
	if err.Error() != (&DiagnosticsRefusal{Reason: DiagnosticsExpired}).Error() {
		t.Fatal("the error text tells the reasons apart")
	}
}
