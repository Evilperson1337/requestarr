//go:build integration

package postgres

import "testing"

func TestUnreadInboxCountIsPerUserAndDropsWhenRead(t *testing.T) {
	ctx, s, _ := integrationStore(t)
	a, b := integrationUser(t, ctx, s), integrationUser(t, ctx, s)
	add := func(user, channel string) string {
		id := newUUID()
		if _, err := s.pool.Exec(ctx, `INSERT INTO notification_deliveries(id,event_key,user_id,channel,status,title,body,created_at,available_at,updated_at) VALUES($1,$2,$3,$4,'pending','t','b',now(),now(),now())`, id, "unread.test:"+id, user, channel); err != nil {
			t.Fatal(err)
		}
		return id
	}
	first := add(a, "in_app")
	add(a, "in_app")
	add(a, "apprise") // not an inbox entry
	add(b, "in_app")
	count := func(user string) int {
		n, err := s.CountUnreadInbox(ctx, user)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	if count(a) != 2 || count(b) != 1 {
		t.Fatalf("counts = %d, %d; want 2, 1", count(a), count(b))
	}
	if ok, err := s.MarkInboxRead(ctx, b, first); err != nil || ok {
		t.Fatal("another person must not be able to read it")
	}
	if ok, err := s.MarkInboxRead(ctx, a, first); err != nil || !ok {
		t.Fatal(err)
	}
	if count(a) != 1 || count(b) != 1 {
		t.Fatalf("after read counts = %d, %d; want 1, 1", count(a), count(b))
	}
}
