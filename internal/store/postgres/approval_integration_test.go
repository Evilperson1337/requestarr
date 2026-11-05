//go:build integration

package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/requests"
)

func TestAutoApprovePolicyStoresValidTypesOnly(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s, err := Open(ctx, testDatabaseURL(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	t.Cleanup(func() { _ = s.SetAutoApproveTypes(context.Background(), nil) })

	if err := s.SetAutoApproveTypes(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if got, err := s.AutoApproveTypes(ctx); err != nil || len(got) != 0 {
		t.Fatalf("default = %v, %v; want none", got, err)
	}
	if err := s.SetAutoApproveTypes(ctx, []domain.MediaType{domain.MediaAudiobook, domain.MediaMovie, domain.MediaMovie}); err != nil {
		t.Fatal(err)
	}
	got, err := s.AutoApproveTypes(ctx)
	if err != nil || len(got) != 2 || got[0] != domain.MediaMovie || got[1] != domain.MediaAudiobook {
		t.Fatalf("stored = %v, %v; want movie, audiobook (deduplicated, in canonical order)", got, err)
	}
	if err := s.SetAutoApproveTypes(ctx, []domain.MediaType{"movie", "podcast"}); !errors.Is(err, ErrInvalidMediaType) {
		t.Fatalf("unknown type error = %v, want ErrInvalidMediaType", err)
	}
	if again, _ := s.AutoApproveTypes(ctx); len(again) != 2 {
		t.Fatalf("a rejected change must not alter the policy, got %v", again)
	}
}

func TestAutoApprovedRequestIsApprovedAuditedAndNotified(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s, err := Open(ctx, testDatabaseURL(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	t.Cleanup(func() { _ = s.SetAutoApproveTypes(context.Background(), nil) })
	svc := requests.New(s)

	newMovie := func() string {
		id := newUUID()
		if _, err := s.pool.Exec(ctx, `INSERT INTO catalog_items(id,media_type,provider,provider_id,title,artwork_kind,external_url,metadata,created_at,updated_at) VALUES($1,'movie','tmdb',$2,'Auto','poster','https://example.test','{}',now(),now())`, id, id); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _, _ = s.pool.Exec(context.Background(), `DELETE FROM catalog_items WHERE id=$1`, id) })
		return id
	}
	create := func(user, item string) requests.CreateResult {
		t.Helper()
		res, err := svc.Create(ctx, requests.CreateInput{Actor: requests.Actor{ID: user, Role: "requester"}, CatalogID: item})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	count := func(query string, args ...any) int {
		t.Helper()
		var n int
		if err := s.pool.QueryRow(ctx, query, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	waiting := func() int {
		n, err := s.AwaitingApprovalCount(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}

	// Policy off: the request waits, with no approval event.
	if err := s.SetAutoApproveTypes(ctx, nil); err != nil {
		t.Fatal(err)
	}
	user := integrationUser(t, ctx, s)
	before := waiting()
	manual := create(user, newMovie())
	if manual.Request.Status != domain.RequestRequested || waiting() != before+1 {
		t.Fatalf("policy off: status %q, waiting %d -> %d", manual.Request.Status, before, waiting())
	}
	if n := count(`SELECT count(*) FROM request_events WHERE request_id=$1 AND event_type='request.approved'`, manual.Request.ID); n != 0 {
		t.Fatalf("policy off recorded %d approval events", n)
	}

	// Approving it by hand still works and leaves the count.
	if _, err := svc.Transition(ctx, requests.TransitionInput{Actor: requests.Actor{ID: user, Role: "admin"}, RequestID: manual.Request.ID, ToStatus: domain.RequestApproved, ExpectedVersion: 1, OperationID: newUUID()}); err != nil {
		t.Fatal(err)
	}
	if waiting() != before {
		t.Fatalf("waiting after manual approval = %d, want %d", waiting(), before)
	}

	// Policy on for movies: created approved, audited as policy, notified.
	pending := create(user, newMovie())
	before = waiting()
	if err := s.SetAutoApproveTypes(ctx, []domain.MediaType{domain.MediaMovie}); err != nil {
		t.Fatal(err)
	}
	item := newMovie()
	auto := create(user, item)
	if auto.Request.Status != domain.RequestApproved || !auto.Created {
		t.Fatalf("auto-approved create = %#v", auto)
	}
	var status string
	if err := s.pool.QueryRow(ctx, `SELECT status FROM requests WHERE id=$1`, auto.Request.ID).Scan(&status); err != nil || status != "approved" {
		t.Fatalf("stored status = %q, %v", status, err)
	}
	if n := count(`SELECT count(*) FROM request_events WHERE request_id=$1 AND event_type='request.approved' AND data->>'auto'='true' AND data->>'reason'='policy'`, auto.Request.ID); n != 1 {
		t.Fatalf("policy approval events = %d, want 1", n)
	}
	if n := count(`SELECT count(*) FROM notification_deliveries WHERE user_id=$1 AND channel='in_app' AND request_event_id IN (SELECT id FROM request_events WHERE request_id=$2 AND event_type='request.approved')`, user, auto.Request.ID); n != 1 {
		t.Fatalf("subscriber approval notices = %d, want 1", n)
	}
	if waiting() != before {
		t.Fatalf("an auto-approved request must not count as waiting: %d, want %d", waiting(), before)
	}

	// A second auto-approved request by the same person is announced too.
	create(user, newMovie())
	if n := count(`SELECT count(*) FROM notification_deliveries WHERE user_id=$1 AND channel='in_app' AND title='Auto' AND body='Your request was approved.' AND request_event_id IN (SELECT id FROM request_events WHERE event_type='request.approved' AND data->>'auto'='true')`, user); n != 2 {
		t.Fatalf("approval notices for two auto-approved requests = %d, want 2", n)
	}

	// A second person joining behaves as before: no new request, no second approval.
	other := integrationUser(t, ctx, s)
	joined := create(other, item)
	if joined.Created || joined.Request.ID != auto.Request.ID {
		t.Fatalf("join = %#v", joined)
	}
	if n := count(`SELECT count(*) FROM request_events WHERE request_id=$1 AND event_type='request.approved'`, auto.Request.ID); n != 1 {
		t.Fatalf("joining added approval events: %d", n)
	}

	// A request that was waiting before the policy was switched on stays waiting.
	var pendingStatus string
	if err := s.pool.QueryRow(ctx, `SELECT status FROM requests WHERE id=$1`, pending.Request.ID).Scan(&pendingStatus); err != nil || pendingStatus != "requested" {
		t.Fatalf("existing request status = %q, %v; want requested", pendingStatus, err)
	}
}
