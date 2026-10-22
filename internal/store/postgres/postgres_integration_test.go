//go:build integration

package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/inventory"
	"github.com/evilperson1337/requestarr/internal/requests"
)

// testDatabaseURL returns the database used only by these integration tests.
// It intentionally reads a dedicated env var, never the application's own
// REQUESTARR_DATABASE_URL, so a test run can never touch a developer's real
// database.
func testDatabaseURL(t *testing.T) string {
	t.Helper()
	url := os.Getenv("REQUESTARR_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("REQUESTARR_TEST_DATABASE_URL not set; skipping PostgreSQL integration test")
	}
	return url
}

func TestOpenAppliesMigrations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	store, err := Open(ctx, testDatabaseURL(t))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	rows, err := store.pool.Query(ctx, `select table_name from information_schema.tables where table_schema = 'public' order by table_name`)
	if err != nil {
		t.Fatalf("query tables: %v", err)
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan table name: %v", err)
		}
		tables = append(tables, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows error: %v", err)
	}

	for _, want := range []string{"users", "oidc_identities", "auth_sessions", "oidc_transactions", "catalog_items", "requests", "request_subscriptions", "request_events", "library_sources", "inventory_items", "notification_deliveries", "notification_preferences", "setup_state", "admin_user_audit_events"} {
		found := false
		for _, got := range tables {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected table %q to exist after migration, tables = %v", want, tables)
		}
	}
}

func TestOpenIsIdempotent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	url := testDatabaseURL(t)

	first, err := Open(ctx, url)
	if err != nil {
		t.Fatalf("first Open() error = %v", err)
	}
	first.Close()

	second, err := Open(ctx, url)
	if err != nil {
		t.Fatalf("second Open() (rerun) error = %v", err)
	}
	defer second.Close()

	if err := second.Ready(ctx); err != nil {
		t.Fatalf("Ready() after rerun = %v", err)
	}
}

func TestAdminRoleChangeProtectsLastAdminAndRevokesSessions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s, err := Open(ctx, testDatabaseURL(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.pool.Exec(ctx, `UPDATE users SET role='requester'`); err != nil {
		t.Fatal(err)
	}
	admin := integrationUser(t, ctx, s)
	requester := integrationUser(t, ctx, s)
	if _, err := s.pool.Exec(ctx, `UPDATE users SET role='admin' WHERE id=$1`, admin); err != nil {
		t.Fatal(err)
	}
	if err := s.ChangeUserRole(ctx, admin, admin, "requester", time.Now().UTC()); !errors.Is(err, ErrAdminSelfRoleChange) {
		t.Fatalf("self role change error = %v", err)
	}
	// Add another administrator and verify session revocation plus atomic audit.
	if _, err := s.pool.Exec(ctx, `UPDATE users SET role='admin' WHERE id=$1`, requester); err != nil {
		t.Fatal(err)
	}
	csrf := sha256.Sum256([]byte(fmt.Sprintf("csrf-%s", requester)))
	token := sha256.Sum256([]byte(fmt.Sprintf("token-%s", requester)))
	if err := s.CreateSession(ctx, requester, token[:], csrf[:], time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := s.ChangeUserRole(ctx, admin, requester, "requester", now); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.GetSession(ctx, token[:], now); err == nil {
		t.Fatal("target session remains valid after role change")
	}
	var count int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM admin_user_audit_events WHERE actor_user_id=$1 AND target_user_id=$2 AND previous_role='admin' AND new_role='requester'`, admin, requester).Scan(&count); err != nil || count != 1 {
		t.Fatalf("audit count=%d err=%v", count, err)
	}
	if err := s.ChangeUserRole(ctx, admin, admin, "requester", now); !errors.Is(err, ErrAdminSelfRoleChange) {
		t.Fatalf("self role change error = %v", err)
	}
	otherAdmin := integrationUser(t, ctx, s)
	if _, err := s.pool.Exec(ctx, `UPDATE users SET role='admin' WHERE id=$1`, otherAdmin); err != nil {
		t.Fatal(err)
	}
	// Keep another admin in place so demoting the original admin exercises the last-admin guard.
	if err := s.ChangeUserRole(ctx, admin, admin, "requester", now); !errors.Is(err, ErrAdminSelfRoleChange) {
		t.Fatalf("self role change error = %v", err)
	}
	if err := s.ChangeUserRole(ctx, otherAdmin, admin, "requester", now); err != nil {
		t.Fatal(err)
	}
	if err := s.ChangeUserRole(ctx, requester, otherAdmin, "requester", now); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("last admin error = %v", err)
	}
}

func TestJellyfinSnapshotIsPositiveIdempotentEvidence(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s, err := Open(ctx, testDatabaseURL(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	id := newUUID()
	_, err = s.pool.Exec(ctx, `INSERT INTO catalog_items(id,media_type,provider,provider_id,title,artwork_kind,external_url,metadata,created_at,updated_at) VALUES($1,'movie','tmdb',$2,'Jellyfin integration','poster','https://example.test','{}',now(),now())`, id, id)
	if err != nil {
		t.Fatal(err)
	}
	defer s.pool.Exec(context.Background(), `DELETE FROM catalog_items WHERE id=$1`, id)
	const source = "5efcc577-5236-4ce3-b2c0-b9d58c64a718"
	snap := inventory.Snapshot{SourceID: source, Complete: true, ObservedAt: time.Now().UTC(), Items: []inventory.Item{{SourceID: source, SourceItemID: "jf-" + id, MediaType: domain.MediaMovie, Title: "Jellyfin integration", ExternalIDs: map[string]string{"tmdb": id}}}}
	for i := 0; i < 2; i++ {
		if err := s.PersistJellyfinSnapshot(ctx, "http://jellyfin.example", snap); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.pool.Exec(ctx, `UPDATE catalog_items SET availability='missing' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if err := s.PersistJellyfinSnapshot(ctx, "http://jellyfin.example", snap); err != nil {
		t.Fatal(err)
	}
	var avail string
	var evidence, events int
	if err := s.pool.QueryRow(ctx, `SELECT availability FROM catalog_items WHERE id=$1`, id).Scan(&avail); err != nil {
		t.Fatal(err)
	}
	_ = s.pool.QueryRow(ctx, `SELECT count(*) FROM availability_evidence WHERE catalog_item_id=$1`, id).Scan(&evidence)
	_ = s.pool.QueryRow(ctx, `SELECT count(*) FROM availability_events WHERE catalog_item_id=$1`, id).Scan(&events)
	if avail != "available" || evidence != 1 || events != 2 {
		t.Fatalf("availability=%s evidence=%d events=%d", avail, evidence, events)
	}
	if err := s.RecordJellyfinFailure(ctx, time.Now().UTC(), "safe failure"); err != nil {
		t.Fatal(err)
	}
	if err := s.pool.QueryRow(ctx, `SELECT availability FROM catalog_items WHERE id=$1`, id).Scan(&avail); err != nil || avail != "available" {
		t.Fatalf("failure changed availability: %s (%v)", avail, err)
	}
}

func TestJellyfinRequiresTwoCompleteAbsencesBeforeMarkingMissing(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s, err := Open(ctx, testDatabaseURL(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	id := newUUID()
	_, err = s.pool.Exec(ctx, `INSERT INTO catalog_items(id,media_type,provider,provider_id,title,artwork_kind,external_url,metadata,availability,created_at,updated_at) VALUES($1,'movie','tmdb',$2,'Negative integration','poster','https://example.test','{}','unknown',now(),now())`, id, id)
	if err != nil {
		t.Fatal(err)
	}
	defer s.pool.Exec(context.Background(), `DELETE FROM catalog_items WHERE id=$1`, id)
	const source = "5efcc577-5236-4ce3-b2c0-b9d58c64a718"
	base := time.Now().UTC().Truncate(time.Second)
	present := inventory.Snapshot{SourceID: source, Complete: true, ObservedAt: base, Items: []inventory.Item{{SourceID: source, SourceItemID: "jf-" + id, MediaType: domain.MediaMovie, Title: "Negative integration", ExternalIDs: map[string]string{"tmdb": id}}}}
	if err := s.PersistJellyfinSnapshot(ctx, "http://jellyfin.example", present); err != nil {
		t.Fatal(err)
	}
	// Distinct successful runs are tracked; same-time duplicate observations do
	// not count as independent absence evidence.
	var version int64
	if err := s.pool.QueryRow(ctx, `SELECT availability_version FROM catalog_items WHERE id=$1`, id).Scan(&version); err != nil {
		t.Fatal(err)
	}
	for _, at := range []time.Time{base.Add(25 * time.Hour), base.Add(50 * time.Hour)} {
		// Non-empty: empty snapshots never count as absence evidence without opt-in.
		other := inventory.Item{SourceID: source, SourceItemID: "other-" + id, MediaType: domain.MediaMovie, Title: "Unrelated"}
		if err := s.PersistJellyfinSnapshot(ctx, "http://jellyfin.example", inventory.Snapshot{SourceID: source, Complete: true, ObservedAt: at, Items: []inventory.Item{other}}); err != nil {
			t.Fatal(err)
		}
	}
	var availability string
	var after int64
	if err := s.pool.QueryRow(ctx, `SELECT availability,availability_version FROM catalog_items WHERE id=$1`, id).Scan(&availability, &after); err != nil {
		t.Fatal(err)
	}
	if availability != "missing" || after != version+1 {
		t.Fatalf("availability=%q version=%d; want missing at version %d", availability, after, version+1)
	}
}

func TestJellyfinEmptySnapshotsRequirePersistedOptIn(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s, err := Open(ctx, testDatabaseURL(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	id := newUUID()
	if _, err := s.pool.Exec(ctx, `INSERT INTO catalog_items(id,media_type,provider,provider_id,title,artwork_kind,external_url,metadata,availability,created_at,updated_at) VALUES($1,'movie','tmdb',$2,'Empty guard','poster','https://example.test','{}','available',now(),now())`, id, id); err != nil {
		t.Fatal(err)
	}
	defer s.pool.Exec(context.Background(), `DELETE FROM catalog_items WHERE id=$1`, id)
	const source = jellyfinSourceID
	if _, err := s.pool.Exec(ctx, `INSERT INTO library_sources(id,kind,name,base_url,enabled,authoritative_media_types,created_at,updated_at) VALUES($1,'jellyfin','Jellyfin','',true,'["movie","tv"]',now(),now()) ON CONFLICT(id) DO NOTHING`, source); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO availability_evidence(catalog_item_id,source_id,source_item_id,observed_at) VALUES($1,$2,'known',now())`, id, source); err != nil {
		t.Fatal(err)
	}
	base := time.Now().UTC().Truncate(time.Second)
	for _, at := range []time.Time{base, base.Add(25 * time.Hour)} {
		if err := s.PersistJellyfinSnapshot(ctx, "http://jellyfin.example", inventory.Snapshot{SourceID: source, Complete: true, ObservedAt: at}); err != nil {
			t.Fatal(err)
		}
	}
	var availability string
	if err := s.pool.QueryRow(ctx, `SELECT availability FROM catalog_items WHERE id=$1`, id).Scan(&availability); err != nil || availability != "available" {
		t.Fatalf("empty snapshots changed availability to %q: %v", availability, err)
	}
}

func TestCreateOrGetDeduplicatesAndRecordsCreatorSubscriptionAndEvent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s, err := Open(ctx, testDatabaseURL(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	// Use a uniquely keyed catalog item so these tests can share a dedicated DB.
	itemID := newUUID()
	_, err = s.pool.Exec(ctx, `INSERT INTO catalog_items(id,media_type,provider,provider_id,title,artwork_kind,external_url,metadata,created_at,updated_at) VALUES($1,'movie','tmdb',$2,'Integration','poster','https://example.test','{}',now(),now())`, itemID, itemID)
	if err != nil {
		t.Fatal(err)
	}
	defer s.pool.Exec(context.Background(), `DELETE FROM catalog_items WHERE id=$1`, itemID)
	creator := integrationUser(t, ctx, s)
	first, created, err := s.CreateOrGet(ctx, domain.Request{CatalogItemID: itemID, Status: domain.RequestRequested, RequestedBy: creator}, domain.RequestSubscription{UserID: creator, Source: domain.SubscriptionRequester})
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("first create unexpectedly deduplicated")
	}
	secondUser := integrationUser(t, ctx, s)
	second, created, err := s.CreateOrGet(ctx, domain.Request{CatalogItemID: itemID, Status: domain.RequestRequested, RequestedBy: secondUser}, domain.RequestSubscription{UserID: secondUser, Source: domain.SubscriptionRequester})
	if err != nil {
		t.Fatal(err)
	}
	if created || second.ID != first.ID || second.RequestedBy != first.RequestedBy {
		t.Fatalf("duplicate = %#v, created=%v; original=%#v", second, created, first)
	}
	var subs, events int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM request_subscriptions WHERE request_id=$1`, first.ID).Scan(&subs); err != nil {
		t.Fatal(err)
	}
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM request_events WHERE request_id=$1 AND event_type='request.created'`, first.ID).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if subs != 1 || events != 1 {
		t.Fatalf("subscriptions=%d events=%d, want 1 each", subs, events)
	}
}

func TestLeavingIsIdempotentAndLastSubscriberCancels(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s, err := Open(ctx, testDatabaseURL(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	itemID := newUUID()
	_, err = s.pool.Exec(ctx, `INSERT INTO catalog_items(id,media_type,provider,provider_id,title,artwork_kind,external_url,metadata,created_at,updated_at) VALUES($1,'movie','tmdb',$2,'Subscription integration','poster','https://example.test','{}',now(),now())`, itemID, itemID)
	if err != nil {
		t.Fatal(err)
	}
	defer s.pool.Exec(context.Background(), `DELETE FROM catalog_items WHERE id=$1`, itemID)
	creator, watcher := integrationUser(t, ctx, s), integrationUser(t, ctx, s)
	r, _, err := s.CreateOrGet(ctx, domain.Request{CatalogItemID: itemID, Status: domain.RequestRequested, RequestedBy: creator}, domain.RequestSubscription{UserID: creator, Source: domain.SubscriptionRequester})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		changed, err := s.Subscribe(ctx, r.ID, watcher)
		if err != nil {
			t.Fatal(err)
		}
		if changed != (i == 0) {
			t.Fatalf("subscribe changed=%v iteration=%d", changed, i)
		}
	}
	leave := func(user string) (bool, bool) {
		t.Helper()
		changed, cancelled, err := s.Unsubscribe(ctx, r.ID, user, true)
		if err != nil {
			t.Fatal(err)
		}
		return changed, cancelled
	}
	state := func() (status string, version int) {
		t.Helper()
		if err := s.pool.QueryRow(ctx, `SELECT status,version FROM requests WHERE id=$1`, r.ID).Scan(&status, &version); err != nil {
			t.Fatal(err)
		}
		return status, version
	}
	// The creator may leave; the Request stays open while the watcher remains.
	if changed, cancelled := leave(creator); !changed || cancelled {
		t.Fatalf("creator leave changed=%v cancelled=%v", changed, cancelled)
	}
	if changed, cancelled := leave(creator); changed || cancelled {
		t.Fatalf("repeat creator leave changed=%v cancelled=%v", changed, cancelled)
	}
	if status, version := state(); status != "requested" || version != 1 {
		t.Fatalf("after non-last leave status=%s version=%d", status, version)
	}
	// Cancelling by leaving needs explicit confirmation; without it nothing changes.
	if changed, cancelled, err := s.Unsubscribe(ctx, r.ID, watcher, false); !errors.Is(err, requests.ErrCancelNeedsConfirmation) || changed || cancelled {
		t.Fatalf("unconfirmed last leave changed=%v cancelled=%v err=%v", changed, cancelled, err)
	}
	if status, version := state(); status != "requested" || version != 1 {
		t.Fatalf("unconfirmed leave altered request: status=%s version=%d", status, version)
	}
	// Confirmed, the last subscriber leaving cancels the Request, once.
	if changed, cancelled := leave(watcher); !changed || !cancelled {
		t.Fatalf("last leave changed=%v cancelled=%v", changed, cancelled)
	}
	if changed, cancelled := leave(watcher); changed || cancelled {
		t.Fatalf("repeat last leave changed=%v cancelled=%v", changed, cancelled)
	}
	if status, version := state(); status != "cancelled" || version != 2 {
		t.Fatalf("after last leave status=%s version=%d", status, version)
	}
	var subscribed, unsubscribed, cancelledEvents int
	if err = s.pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE event_type='request.subscribed'),count(*) FILTER (WHERE event_type='request.unsubscribed'),count(*) FILTER (WHERE event_type='request.cancelled') FROM request_events WHERE request_id=$1`, r.ID).Scan(&subscribed, &unsubscribed, &cancelledEvents); err != nil {
		t.Fatal(err)
	}
	if subscribed != 1 || unsubscribed != 2 || cancelledEvents != 1 {
		t.Fatalf("events subscribed=%d unsubscribed=%d cancelled=%d, want 1,2,1", subscribed, unsubscribed, cancelledEvents)
	}
}

func TestConcurrentTransitionsCommitSingleSequencedAuditAndRejectStaleVersion(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s, err := Open(ctx, testDatabaseURL(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	itemID := newUUID()
	if _, err = s.pool.Exec(ctx, `INSERT INTO catalog_items(id,media_type,provider,provider_id,title,artwork_kind,external_url,metadata,created_at,updated_at) VALUES($1,'movie','tmdb',$2,'Transition integration','poster','https://example.test','{}',now(),now())`, itemID, itemID); err != nil {
		t.Fatal(err)
	}
	defer s.pool.Exec(context.Background(), `DELETE FROM catalog_items WHERE id=$1`, itemID)
	admin := integrationUser(t, ctx, s)
	if _, err = s.pool.Exec(ctx, `UPDATE users SET role='admin' WHERE id=$1`, admin); err != nil {
		t.Fatal(err)
	}
	r, _, err := s.CreateOrGet(ctx, domain.Request{CatalogItemID: itemID, Status: domain.RequestRequested, RequestedBy: admin}, domain.RequestSubscription{UserID: admin, Source: domain.SubscriptionRequester})
	if err != nil {
		t.Fatal(err)
	}
	inputs := []requests.TransitionInput{{Actor: requests.Actor{ID: admin, Role: "admin"}, RequestID: r.ID, ToStatus: domain.RequestApproved, ExpectedVersion: 1, OperationID: newUUID()}, {Actor: requests.Actor{ID: admin, Role: "admin"}, RequestID: r.ID, ToStatus: domain.RequestDenied, ExpectedVersion: 1, OperationID: newUUID()}}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	successes := make(chan struct{}, 2)
	for _, in := range inputs {
		in := in
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, e := s.Transition(ctx, in)
			if e == nil {
				successes <- struct{}{}
			} else {
				errs <- e
			}
		}()
	}
	wg.Wait()
	close(successes)
	close(errs)
	if len(successes) != 1 {
		t.Fatalf("successful transitions=%d, want exactly one", len(successes))
	}
	conflicts := 0
	for e := range errs {
		if errors.Is(e, requests.ErrTransitionConflict) {
			conflicts++
		} else {
			t.Fatalf("unexpected transition error: %v", e)
		}
	}
	if conflicts != 1 {
		t.Fatalf("conflicts=%d, want 1", conflicts)
	}
	var status string
	var version int64
	var available string
	var events int
	if err = s.pool.QueryRow(ctx, `SELECT status,version,availability_state FROM requests WHERE id=$1`, r.ID).Scan(&status, &version, &available); err != nil {
		t.Fatal(err)
	}
	if err = s.pool.QueryRow(ctx, `SELECT count(*) FROM request_events WHERE request_id=$1 AND event_type IN ('request.approved','request.denied')`, r.ID).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if version != 2 || events != 1 || available != "unknown" || (status != "approved" && status != "denied") {
		t.Fatalf("status=%s version=%d events=%d availability=%s", status, version, events, available)
	}
}

func integrationUser(t *testing.T, ctx context.Context, s *Store) string {
	t.Helper()
	id := newUUID()
	_, err := s.pool.Exec(ctx, `INSERT INTO users(id,oidc_subject,email,display_name,role,created_at,updated_at) VALUES($1,$2,$2,'Integration','requester',now(),now())`, id, "integration:"+id)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = s.pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, id) })
	return id
}

func TestLoginProvisioningUsesMigratedSchemaAndIsConcurrentSafe(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s, err := Open(ctx, testDatabaseURL(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	const issuer = "https://issuer.example"
	subject := "stable-subject-" + newUUID()
	var wg sync.WaitGroup
	users := make(chan string, 2)
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			u, err := s.Login(ctx, issuer, subject, "person@example.test", "Person", newUUID(), time.Now().UTC())
			if err != nil {
				errs <- err
				return
			}
			users <- u.ID
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	close(users)
	var first string
	for id := range users {
		if first == "" {
			first = id
			continue
		}
		if id != first {
			t.Fatalf("concurrent login created distinct users %q and %q", first, id)
		}
	}
	var count int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM users u JOIN oidc_identities i ON i.user_id=u.id WHERE i.issuer=$1 AND i.subject=$2 AND u.email=$3`, issuer, subject, "person@example.test").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("users for identity = %d, want 1", count)
	}
	var role string
	if err := s.pool.QueryRow(ctx, `SELECT role FROM users WHERE id=$1`, first).Scan(&role); err != nil {
		t.Fatal(err)
	}
	if role != "viewer" {
		t.Fatalf("new identity role = %q, want viewer", role)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE users SET role='requester' WHERE id=$1`, first); err != nil {
		t.Fatal(err)
	}
	updated, err := s.Login(ctx, issuer, subject, "person@example.test", "Person Updated", newUUID(), time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != first || updated.Role != "requester" {
		t.Fatalf("repeated login user = %#v, want same ID with preserved requester role", updated)
	}
	var storedSubject, storedEmail, storedIssuer string
	if err := s.pool.QueryRow(ctx, `SELECT u.oidc_subject,u.email,i.issuer FROM users u JOIN oidc_identities i ON i.user_id=u.id WHERE i.issuer=$1 AND i.subject=$2`, issuer, subject).Scan(&storedSubject, &storedEmail, &storedIssuer); err != nil {
		t.Fatal(err)
	}
	identity := append(append(append([]byte(nil), issuer...), 0), subject...)
	digest := sha256.Sum256(identity)
	wantLegacyKey := "oidc:" + hex.EncodeToString(digest[:])
	if storedSubject != wantLegacyKey || storedEmail != "person@example.test" || storedIssuer != issuer {
		t.Fatalf("stored identity key/email/issuer = %q/%q/%q, want %q/%q/%q", storedSubject, storedEmail, storedIssuer, wantLegacyKey, "person@example.test", issuer)
	}
}

func TestAdminCanDemoteToViewerButNotTheLastAdmin(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s, err := Open(ctx, testDatabaseURL(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.pool.Exec(ctx, `UPDATE users SET role='requester'`); err != nil {
		t.Fatal(err)
	}
	adminOne := integrationUser(t, ctx, s)
	adminTwo := integrationUser(t, ctx, s)
	if _, err := s.pool.Exec(ctx, `UPDATE users SET role='admin' WHERE id IN ($1,$2)`, adminOne, adminTwo); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := s.ChangeUserRole(ctx, adminOne, adminTwo, "viewer", now); err != nil {
		t.Fatalf("demote to viewer: %v", err)
	}
	var role string
	if err := s.pool.QueryRow(ctx, `SELECT role FROM users WHERE id=$1`, adminTwo).Scan(&role); err != nil || role != "viewer" {
		t.Fatalf("role=%q err=%v", role, err)
	}
	var audits int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM admin_user_audit_events WHERE target_user_id=$1 AND previous_role='admin' AND new_role='viewer'`, adminTwo).Scan(&audits); err != nil || audits != 1 {
		t.Fatalf("audit count=%d err=%v", audits, err)
	}
	// adminOne is now the only administrator; another actor cannot demote them to viewer.
	csrf := sha256.Sum256([]byte(fmt.Sprintf("csrf-%s", adminOne)))
	token := sha256.Sum256([]byte(fmt.Sprintf("token-%s", adminOne)))
	if err := s.CreateSession(ctx, adminOne, token[:], csrf[:], now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := s.ChangeUserRole(ctx, adminTwo, adminOne, "viewer", now); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("last admin demotion error = %v", err)
	}
	if err := s.pool.QueryRow(ctx, `SELECT role FROM users WHERE id=$1`, adminOne).Scan(&role); err != nil || role != "admin" {
		t.Fatalf("last admin role=%q err=%v, want unchanged", role, err)
	}
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM admin_user_audit_events WHERE target_user_id=$1`, adminOne).Scan(&audits); err != nil || audits != 0 {
		t.Fatalf("refused demotion wrote %d audit rows, err=%v", audits, err)
	}
	var revoked int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM auth_sessions WHERE user_id=$1 AND revoked_at IS NOT NULL`, adminOne).Scan(&revoked); err != nil || revoked != 0 {
		t.Fatalf("refused demotion revoked %d sessions, err=%v", revoked, err)
	}
	if err := s.ChangeUserRole(ctx, adminOne, adminTwo, "requester", now); err != nil {
		t.Fatalf("viewer to requester: %v", err)
	}
}

func jellyfinSnapshotFor(catalogID string, complete bool) inventory.Snapshot {
	return inventory.Snapshot{SourceID: jellyfinSourceID, Complete: complete, ObservedAt: time.Now().UTC(), Items: []inventory.Item{{SourceID: jellyfinSourceID, SourceItemID: "jf-" + catalogID, MediaType: domain.MediaMovie, Title: "Availability integration", ExternalIDs: map[string]string{"tmdb": catalogID}}}}
}

func newMovieWithRequest(t *testing.T, ctx context.Context, s *Store, status domain.RequestStatus) (catalogID, requestID, requester string) {
	t.Helper()
	catalogID = newUUID()
	if _, err := s.pool.Exec(ctx, `INSERT INTO catalog_items(id,media_type,provider,provider_id,title,artwork_kind,external_url,metadata,created_at,updated_at) VALUES($1,'movie','tmdb',$2,'Availability integration','poster','https://example.test','{}',now(),now())`, catalogID, catalogID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = s.pool.Exec(context.Background(), `DELETE FROM catalog_items WHERE id=$1`, catalogID) })
	requester = integrationUser(t, ctx, s)
	req, _, err := s.CreateOrGet(ctx, domain.Request{CatalogItemID: catalogID, Status: domain.RequestRequested, RequestedBy: requester}, domain.RequestSubscription{UserID: requester, Source: domain.SubscriptionRequester})
	if err != nil {
		t.Fatal(err)
	}
	if status != domain.RequestRequested {
		if _, err := s.pool.Exec(ctx, `UPDATE requests SET status=$2 WHERE id=$1`, req.ID, status); err != nil {
			t.Fatal(err)
		}
	}
	return catalogID, req.ID, requester
}

type requestEvidence struct {
	status                                     string
	version                                    int64
	availableAt                                bool
	auditEvents, inApp, apprise, catalogEvents int
}

func readRequestEvidence(t *testing.T, ctx context.Context, s *Store, catalogID, requestID string) requestEvidence {
	t.Helper()
	var e requestEvidence
	if err := s.pool.QueryRow(ctx, `SELECT status,version,available_at IS NOT NULL FROM requests WHERE id=$1`, requestID).Scan(&e.status, &e.version, &e.availableAt); err != nil {
		t.Fatal(err)
	}
	key := "request.available:" + requestID
	for query, dst := range map[string]*int{
		`SELECT count(*) FROM request_events WHERE request_id='` + requestID + `' AND event_type='request.available'`:                &e.auditEvents,
		`SELECT count(*) FROM notification_deliveries WHERE channel='in_app' AND event_key='` + key + `'`:                            &e.inApp,
		`SELECT count(*) FROM notification_outbox WHERE event_key='` + key + `'`:                                                     &e.apprise,
		`SELECT count(*) FROM availability_events WHERE catalog_item_id='` + catalogID + `' AND event_type='availability.available'`: &e.catalogEvents,
	} {
		if err := s.pool.QueryRow(ctx, query).Scan(dst); err != nil {
			t.Fatal(err)
		}
	}
	return e
}

func TestJellyfinEvidenceMovesOpenRequestsToAvailableOnce(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s, err := Open(ctx, testDatabaseURL(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, open := range []domain.RequestStatus{domain.RequestRequested, domain.RequestApproved} {
		t.Run(string(open), func(t *testing.T) {
			catalogID, requestID, _ := newMovieWithRequest(t, ctx, s, open)
			subscriber := integrationUser(t, ctx, s)
			if _, err := s.pool.Exec(ctx, `INSERT INTO request_subscriptions(request_id,user_id,source,created_at) VALUES($1,$2,'manual',now())`, requestID, subscriber); err != nil {
				t.Fatal(err)
			}
			before := readRequestEvidence(t, ctx, s, catalogID, requestID)
			snap := jellyfinSnapshotFor(catalogID, true)
			for run := 1; run <= 2; run++ {
				if err := s.PersistJellyfinSnapshot(ctx, "http://jellyfin.example", snap); err != nil {
					t.Fatal(err)
				}
				got := readRequestEvidence(t, ctx, s, catalogID, requestID)
				want := requestEvidence{status: "available", version: before.version + 1, availableAt: true, auditEvents: 1, inApp: 2, apprise: 1, catalogEvents: 1}
				if got != want {
					t.Fatalf("after run %d: got %+v want %+v", run, got, want)
				}
			}
		})
	}
}

func TestJellyfinSnapshotLeavesClosedAndUnmatchedRequestsAndFailedSyncsAlone(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s, err := Open(ctx, testDatabaseURL(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	deniedCatalog, deniedRequest, _ := newMovieWithRequest(t, ctx, s, domain.RequestDenied)
	if err := s.PersistJellyfinSnapshot(ctx, "http://jellyfin.example", jellyfinSnapshotFor(deniedCatalog, true)); err != nil {
		t.Fatal(err)
	}
	if got := readRequestEvidence(t, ctx, s, deniedCatalog, deniedRequest); got.status != "denied" || got.auditEvents != 0 || got.inApp != 0 || got.apprise != 0 {
		t.Fatalf("denied request changed: %+v", got)
	}

	openCatalog, openRequest, _ := newMovieWithRequest(t, ctx, s, domain.RequestRequested)
	before := readRequestEvidence(t, ctx, s, openCatalog, openRequest)
	if err := s.PersistJellyfinSnapshot(ctx, "http://jellyfin.example", jellyfinSnapshotFor(openCatalog, false)); err == nil {
		t.Fatal("incomplete snapshot was accepted")
	}
	if err := s.RecordJellyfinFailure(ctx, time.Now().UTC(), "safe failure"); err != nil {
		t.Fatal(err)
	}
	if after := readRequestEvidence(t, ctx, s, openCatalog, openRequest); after != before {
		t.Fatalf("failed sync changed state: before %+v after %+v", before, after)
	}
	var availability string
	var evidence int
	if err := s.pool.QueryRow(ctx, `SELECT availability,(SELECT count(*) FROM availability_evidence WHERE catalog_item_id=$1) FROM catalog_items WHERE id=$1`, openCatalog).Scan(&availability, &evidence); err != nil {
		t.Fatal(err)
	}
	if availability != "unknown" || evidence != 0 {
		t.Fatalf("failed sync wrote availability=%q evidence=%d", availability, evidence)
	}
}

func TestAdminCancelsOpenRequestsOutright(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s, err := Open(ctx, testDatabaseURL(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	admin := integrationUser(t, ctx, s)
	for _, from := range []domain.RequestStatus{domain.RequestRequested, domain.RequestApproved} {
		t.Run(string(from), func(t *testing.T) {
			_, requestID, _ := newMovieWithRequest(t, ctx, s, from)
			in := requests.TransitionInput{Actor: requests.Actor{ID: admin, Role: "admin"}, RequestID: requestID, ToStatus: domain.RequestCancelled, ExpectedVersion: 1, OperationID: newUUID()}
			if _, _, err := s.Transition(ctx, requests.TransitionInput{Actor: in.Actor, RequestID: requestID, ToStatus: domain.RequestCancelled, ExpectedVersion: 9, OperationID: newUUID()}); !errors.Is(err, requests.ErrTransitionConflict) {
				t.Fatalf("stale version error=%v", err)
			}
			got, changed, err := s.Transition(ctx, in)
			if err != nil || !changed || got.Status != domain.RequestCancelled || got.Version != 2 {
				t.Fatalf("cancel got=%+v changed=%v err=%v", got, changed, err)
			}
			var events, inApp int
			if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM request_events WHERE request_id=$1 AND event_type='request.cancelled' AND operation_id=$2`, requestID, in.OperationID).Scan(&events); err != nil {
				t.Fatal(err)
			}
			if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM notification_deliveries WHERE channel='in_app' AND event_key='request.cancelled' AND request_event_id IN (SELECT id FROM request_events WHERE request_id=$1)`, requestID).Scan(&inApp); err != nil {
				t.Fatal(err)
			}
			if events != 1 || inApp != 1 {
				t.Fatalf("audit events=%d subscriber notifications=%d, want 1 and 1", events, inApp)
			}
			again := requests.TransitionInput{Actor: in.Actor, RequestID: requestID, ToStatus: domain.RequestCancelled, ExpectedVersion: 2, OperationID: newUUID()}
			if _, _, err := s.Transition(ctx, again); !errors.Is(err, requests.ErrTransitionInvalid) {
				t.Fatalf("cancelling a closed request error=%v", err)
			}
		})
	}
}

func TestClosedRequestsRejectNewSubscribersAndReplayedCancelsAreConflicts(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s, err := Open(ctx, testDatabaseURL(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	admin := integrationUser(t, ctx, s)
	_, requestID, _ := newMovieWithRequest(t, ctx, s, domain.RequestRequested)
	in := requests.TransitionInput{Actor: requests.Actor{ID: admin, Role: "admin"}, RequestID: requestID, ToStatus: domain.RequestCancelled, ExpectedVersion: 1, OperationID: newUUID()}
	if _, _, err := s.Transition(ctx, in); err != nil {
		t.Fatal(err)
	}
	// Replaying the identical operation must not append a second event.
	if _, changed, err := s.Transition(ctx, in); !errors.Is(err, requests.ErrTransitionConflict) || changed {
		t.Fatalf("replay changed=%v err=%v, want version conflict", changed, err)
	}
	var events int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM request_events WHERE request_id=$1 AND operation_id=$2`, requestID, in.OperationID).Scan(&events); err != nil || events != 1 {
		t.Fatalf("events for operation=%d err=%v, want 1", events, err)
	}
	watcher := integrationUser(t, ctx, s)
	if changed, err := s.Subscribe(ctx, requestID, watcher); !errors.Is(err, requests.ErrRequestClosed) || changed {
		t.Fatalf("subscribe to cancelled changed=%v err=%v", changed, err)
	}
	var subs int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM request_subscriptions WHERE request_id=$1 AND user_id=$2`, requestID, watcher).Scan(&subs); err != nil || subs != 0 {
		t.Fatalf("closed request gained subscriber: %d %v", subs, err)
	}
}
