//go:build integration

package postgres

import (
	"context"
	"testing"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/requests"
)

func TestRequestMadeOnBehalfIsThePersonsAndRecordsTheAdministrator(t *testing.T) {
	ctx, s, admin := integrationStore(t)
	t.Cleanup(func() { _ = s.SetAutoApproveTypes(context.Background(), nil) })
	person := integrationUser(t, ctx, s)
	other := integrationUser(t, ctx, s)
	svc := requests.New(s)

	item := newUUID()
	if _, err := s.pool.Exec(ctx, `INSERT INTO catalog_items(id,media_type,provider,provider_id,title,artwork_kind,external_url,metadata,created_at,updated_at) VALUES($1,'movie','tmdb',$2,'Behalf','poster','https://example.test','{}',now(),now())`, item, item); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = s.pool.Exec(context.Background(), `DELETE FROM catalog_items WHERE id=$1`, item) })
	if err := s.SetAutoApproveTypes(ctx, []domain.MediaType{domain.MediaMovie}); err != nil {
		t.Fatal(err)
	}

	res, err := svc.Create(ctx, requests.CreateInput{Actor: requests.Actor{ID: admin, Role: "admin"}, OnBehalfOf: person, CatalogID: item})
	if err != nil || !res.Created {
		t.Fatalf("create = %#v, %v", res, err)
	}
	if res.Request.RequestedBy != person || res.Request.Status != domain.RequestApproved {
		t.Fatalf("request = %#v, want the person's, approved by policy", res.Request)
	}
	var source string
	if err := s.pool.QueryRow(ctx, `SELECT source FROM request_subscriptions WHERE request_id=$1 AND user_id=$2`, res.Request.ID, person).Scan(&source); err != nil || source != "requester" {
		t.Fatalf("person subscription = %q, %v", source, err)
	}
	var n int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM request_subscriptions WHERE request_id=$1 AND user_id=$2`, res.Request.ID, admin).Scan(&n); err != nil || n != 0 {
		t.Fatalf("the administrator was subscribed (%d, %v)", n, err)
	}
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM notification_deliveries WHERE user_id=$1 AND channel='in_app' AND request_event_id IN (SELECT id FROM request_events WHERE request_id=$2)`, person, res.Request.ID).Scan(&n); err != nil || n < 1 {
		t.Fatalf("person notices = %d, %v", n, err)
	}

	// The list shows who made it on their behalf, by name.
	page, err := s.List(ctx, person, requests.ListQuery{})
	if err != nil {
		t.Fatal(err)
	}
	var listed *domain.Request
	for i := range page.Items {
		if page.Items[i].ID == res.Request.ID {
			listed = &page.Items[i]
		}
	}
	if listed == nil || listed.MadeOnBehalfBy != admin || listed.MadeOnBehalfByName == "" {
		t.Fatalf("listed = %#v", listed)
	}

	// A second request for the same item subscribes the other person instead.
	again, err := svc.Create(ctx, requests.CreateInput{Actor: requests.Actor{ID: admin, Role: "admin"}, OnBehalfOf: other, CatalogID: item})
	if err != nil || again.Created || again.Request.ID != res.Request.ID {
		t.Fatalf("second create = %#v, %v", again, err)
	}
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM requests WHERE catalog_item_id=$1`, item).Scan(&n); err != nil || n != 1 {
		t.Fatalf("requests for the item = %d, %v", n, err)
	}
	if err := s.pool.QueryRow(ctx, `SELECT source FROM request_subscriptions WHERE request_id=$1 AND user_id=$2`, res.Request.ID, other).Scan(&source); err != nil || source != "manual" {
		t.Fatalf("other subscription = %q, %v", source, err)
	}

	// An unknown person is refused.
	if _, err := svc.Create(ctx, requests.CreateInput{Actor: requests.Actor{ID: admin, Role: "admin"}, OnBehalfOf: newUUID(), CatalogID: item}); err == nil {
		t.Fatal("unknown person accepted")
	}
}
