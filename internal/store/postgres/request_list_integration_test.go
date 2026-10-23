//go:build integration

package postgres

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/requests"
)

type queueFixture struct {
	user, prefix string
	ids          []string
	catalogIDs   []string
}

func newQueueFixture(t *testing.T, ctx context.Context, s *Store) queueFixture {
	t.Helper()
	f := queueFixture{user: integrationUser(t, ctx, s), prefix: "Queue " + newUUID()}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for index, spec := range []struct {
		media  string
		status domain.RequestStatus
		mine   bool
	}{
		{"movie", domain.RequestRequested, true},
		{"movie", domain.RequestRequested, true},
		{"movie", domain.RequestRequested, false},
		{"tv", domain.RequestRequested, true},
		{"movie", domain.RequestApproved, true},
		{"song", domain.RequestAvailable, false},
	} {
		catalogID, requestID, _ := newMovieWithRequest(t, ctx, s, spec.status)
		if _, err := s.pool.Exec(ctx, `UPDATE catalog_items SET title=$2,media_type=$3 WHERE id=$1`, catalogID, f.prefix+" Fleet", spec.media); err != nil {
			t.Fatal(err)
		}
		if _, err := s.pool.Exec(ctx, `UPDATE requests SET requested_at=$2 WHERE id=$1`, requestID, base.Add(time.Duration(index)*time.Hour)); err != nil {
			t.Fatal(err)
		}
		if spec.mine {
			if _, err := s.Subscribe(ctx, requestID, f.user); err != nil {
				t.Fatal(err)
			}
		}
		f.ids = append(f.ids, requestID)
		f.catalogIDs = append(f.catalogIDs, catalogID)
	}
	return f
}

func TestRequestListCombinesTitleStatusMediaAndMine(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s := openJobStore(t, ctx)
	f := newQueueFixture(t, ctx, s)
	page, err := s.List(ctx, f.user, requests.ListQuery{Q: f.prefix + " FLEET", Status: domain.RequestRequested, MediaType: domain.MediaMovie, Mine: true, Sort: "oldest"})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || page.Items[0].ID != f.ids[0] || page.Items[1].ID != f.ids[1] || page.NextCursor != nil {
		t.Fatalf("combined filters: %+v", page)
	}
	for _, item := range page.Items {
		if !item.Subscribed || item.RequestedBy == f.user {
			t.Fatalf("mine must include manual subscriptions, not just creators: %+v", item)
		}
	}
}

func TestRequestPaginationFreezesMembershipAndOrderWhileRequestsChange(t *testing.T) {
	for _, sort := range []string{"newest", "oldest", "status"} {
		t.Run(sort, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			s := openJobStore(t, ctx)
			f := newQueueFixture(t, ctx, s)
			query := requests.ListQuery{Q: f.prefix, Status: domain.RequestRequested, Mine: true, Sort: sort, Limit: 1}
			page, err := s.List(ctx, f.user, query)
			if err != nil || len(page.Items) != 1 || page.NextCursor == nil {
				t.Fatalf("first page: %+v err=%v", page, err)
			}
			got := []string{page.Items[0].ID}
			// This unseen row stops matching all three mutable filters. It must
			// remain in the snapshot but carry its current state when returned.
			if _, err := s.pool.Exec(ctx, `UPDATE requests SET status='denied' WHERE id=$1`, f.ids[1]); err != nil {
				t.Fatal(err)
			}
			if _, err := s.pool.Exec(ctx, `UPDATE catalog_items SET title='Changed after first page' WHERE id=$1`, f.catalogIDs[1]); err != nil {
				t.Fatal(err)
			}
			if _, _, err := s.Unsubscribe(ctx, f.ids[1], f.user, false); err != nil {
				t.Fatal(err)
			}
			// Neither a newly eligible subscription nor a new Request may enter
			// this traversal; refreshing the view is what includes them.
			if _, err := s.Subscribe(ctx, f.ids[2], f.user); err != nil {
				t.Fatal(err)
			}
			catalogID, requestID, _ := newMovieWithRequest(t, ctx, s, domain.RequestRequested)
			if _, err := s.pool.Exec(ctx, `UPDATE catalog_items SET title=$2 WHERE id=$1`, catalogID, f.prefix+" New"); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Subscribe(ctx, requestID, f.user); err != nil {
				t.Fatal(err)
			}
			// A separately opened Store must be able to continue the cursor.
			reader := openJobStore(t, ctx)
			for page.NextCursor != nil {
				query.Cursor = *page.NextCursor
				page, err = reader.List(ctx, f.user, query)
				if err != nil || len(page.Items) != 1 {
					t.Fatalf("next page: %+v err=%v", page, err)
				}
				item := page.Items[0]
				got = append(got, item.ID)
				if item.ID == f.ids[1] && (item.Status != domain.RequestDenied || item.Title != "Changed after first page" || item.Subscribed) {
					t.Fatalf("snapshot returned stale details: %+v", item)
				}
				if len(got) > 3 {
					t.Fatal("snapshot repeated or admitted rows")
				}
			}
			want := []string{f.ids[3], f.ids[1], f.ids[0]}
			if sort == "oldest" {
				want = []string{f.ids[0], f.ids[1], f.ids[3]}
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("unstable traversal: got %v want %v", got, want)
			}
			query.Cursor = ""
			query.Limit = 100
			fresh, err := reader.List(ctx, f.user, query)
			if err != nil || len(fresh.Items) != 4 {
				t.Fatalf("refresh did not pick up current membership: %+v err=%v", fresh, err)
			}
		})
	}
}

func TestRequestListSortsAllStatusesAndBreaksTiesByID(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s := openJobStore(t, ctx)
	f := newQueueFixture(t, ctx, s)
	for index, status := range map[int]string{2: "denied", 3: "cancelled"} {
		if _, err := s.pool.Exec(ctx, `UPDATE requests SET status=$2 WHERE id=$1`, f.ids[index], status); err != nil {
			t.Fatal(err)
		}
	}
	page, err := s.List(ctx, f.user, requests.ListQuery{Q: f.prefix, Sort: "status"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{f.ids[1], f.ids[0], f.ids[4], f.ids[5], f.ids[2], f.ids[3]}
	if got := requestIDs(page.Items); !reflect.DeepEqual(got, want) {
		t.Fatalf("status order: got %v want %v", got, want)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE requests SET requested_at='2026-01-01T00:00:00Z',status='requested' WHERE id=ANY($1::uuid[])`, f.ids); err != nil {
		t.Fatal(err)
	}
	want = append([]string(nil), f.ids...)
	sort.Strings(want)
	for _, ordering := range []string{"newest", "oldest", "status"} {
		query := requests.ListQuery{Q: f.prefix, Sort: ordering, Limit: 2}
		var got []string
		for {
			page, err = s.List(ctx, f.user, query)
			if err != nil {
				t.Fatal(err)
			}
			got = append(got, requestIDs(page.Items)...)
			if page.NextCursor == nil {
				break
			}
			query.Cursor = *page.NextCursor
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s ties: got %v want %v", ordering, got, want)
		}
	}
}

func TestRequestListSearchIsLiteralAndFiltersCoverAllMediaAndStatuses(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s := openJobStore(t, ctx)
	f := newQueueFixture(t, ctx, s)
	pairs := []struct{ media, status string }{{"movie", "requested"}, {"tv", "approved"}, {"album", "available"}, {"song", "denied"}, {"game", "cancelled"}}
	for index, pair := range pairs {
		if _, err := s.pool.Exec(ctx, `UPDATE catalog_items SET media_type=$2 WHERE id=$1`, f.catalogIDs[index], pair.media); err != nil {
			t.Fatal(err)
		}
		if _, err := s.pool.Exec(ctx, `UPDATE requests SET status=$2 WHERE id=$1`, f.ids[index], pair.status); err != nil {
			t.Fatal(err)
		}
	}
	for index, pair := range pairs {
		page, err := s.List(ctx, f.user, requests.ListQuery{Q: f.prefix, MediaType: domain.MediaType(pair.media), Status: domain.RequestStatus(pair.status)})
		if err != nil || len(page.Items) != 1 || page.Items[0].ID != f.ids[index] {
			t.Fatalf("filter %v: %+v err=%v", pair, page, err)
		}
	}
	if _, err := s.pool.Exec(ctx, `UPDATE catalog_items SET title=$2 WHERE id=$1`, f.catalogIDs[0], f.prefix+` 100%_\ Étoile`); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{f.prefix + ` 100%_\`, f.prefix + ` 100%_\ ÉTOILE`, f.prefix + `' OR true --`} {
		page, err := s.List(ctx, f.user, requests.ListQuery{Q: text})
		if err != nil {
			t.Fatal(err)
		}
		want := 1
		if strings.Contains(text, "OR true") {
			want = 0
		}
		if len(page.Items) != want {
			t.Fatalf("literal search %q returned %+v", text, page.Items)
		}
	}
}

func TestRequestCursorsAreScopedSignedReplayableAndExpire(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s := openJobStore(t, ctx)
	f := newQueueFixture(t, ctx, s)
	query := requests.ListQuery{Q: f.prefix, Limit: 2}
	first, err := s.List(ctx, f.user, query)
	if err != nil || first.NextCursor == nil {
		t.Fatalf("first page: %+v err=%v", first, err)
	}
	query.Cursor = *first.NextCursor
	next, err := s.List(ctx, f.user, query)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := s.List(ctx, f.user, query)
	if err != nil || !reflect.DeepEqual(next, replay) {
		t.Fatalf("cursor replay changed page: first=%+v retry=%+v err=%v", next, replay, err)
	}
	for _, change := range []func(*requests.ListQuery){
		func(q *requests.ListQuery) { q.Q += " different" },
		func(q *requests.ListQuery) { q.Status = domain.RequestRequested },
		func(q *requests.ListQuery) { q.MediaType = domain.MediaMovie },
		func(q *requests.ListQuery) { q.Mine = true },
		func(q *requests.ListQuery) { q.Sort = "oldest" },
		func(q *requests.ListQuery) { q.Limit = 3 },
	} {
		changed := query
		change(&changed)
		if _, err := s.List(ctx, f.user, changed); !errors.Is(err, requests.ErrInvalidCursor) {
			t.Fatalf("changed query accepted cursor: %+v err=%v", changed, err)
		}
	}
	other := integrationUser(t, ctx, s)
	if _, err := s.List(ctx, other, query); !errors.Is(err, requests.ErrInvalidCursor) {
		t.Fatalf("cross-user cursor accepted: %v", err)
	}
	parts := strings.Split(query.Cursor, ".")
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatal(err)
	}
	var edited map[string]any
	if err := json.Unmarshal(payload, &edited); err != nil {
		t.Fatal(err)
	}
	edited["p"] = 4
	tampered, err := json.Marshal(edited)
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"not-a-cursor", strings.Repeat("x", 513), base64.RawURLEncoding.EncodeToString(tampered) + "." + parts[1]} {
		invalid := query
		invalid.Cursor = token
		if _, err := s.List(ctx, f.user, invalid); !errors.Is(err, requests.ErrInvalidCursor) {
			t.Fatalf("malformed/tampered cursor accepted: %v", err)
		}
	}
	if _, err := s.pool.Exec(ctx, `UPDATE request_list_snapshots SET expires_at=clock_timestamp()-interval '1 second' WHERE user_id=$1`, f.user); err != nil {
		t.Fatal(err)
	}
	if _, err := s.List(ctx, f.user, query); !errors.Is(err, requests.ErrCursorExpired) {
		t.Fatalf("expired cursor accepted: %v", err)
	}
}

func TestRequestSnapshotRetentionIsBounded(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s := openJobStore(t, ctx)
	f := newQueueFixture(t, ctx, s)
	query := requests.ListQuery{Q: f.prefix, Limit: 1}
	var first string
	for range 6 {
		page, err := s.List(ctx, f.user, query)
		if err != nil || page.NextCursor == nil {
			t.Fatalf("first page: %+v err=%v", page, err)
		}
		if first == "" {
			first = *page.NextCursor
		}
	}
	query.Cursor = first
	if _, err := s.List(ctx, f.user, query); !errors.Is(err, requests.ErrCursorExpired) {
		t.Fatalf("oldest snapshot was not evicted: %v", err)
	}
	var count int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM request_list_snapshots WHERE user_id=$1`, f.user).Scan(&count); err != nil || count != 5 {
		t.Fatalf("snapshot retention count=%d err=%v", count, err)
	}
}

func TestRequestSnapshotCapacityFailsWithoutSilentlyTruncating(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s := openJobStore(t, ctx)
	user, prefix := integrationUser(t, ctx, s), "Capacity "+newUUID()
	t.Cleanup(func() {
		_, _ = s.pool.Exec(context.Background(), `DELETE FROM requests WHERE requested_by=$1`, user)
		_, _ = s.pool.Exec(context.Background(), `DELETE FROM catalog_items WHERE title=$1`, prefix)
	})
	if _, err := s.pool.Exec(ctx, `WITH inserted AS (
		INSERT INTO catalog_items(id,media_type,provider,provider_id,title,artwork_kind,external_url,metadata,created_at,updated_at)
		SELECT gen_random_uuid(),'movie','tmdb',gen_random_uuid()::text,$1,'poster','https://example.test','{}',now(),now()
		FROM generate_series(1,10001) RETURNING id
	) INSERT INTO requests(id,catalog_item_id,status,requested_by,requested_at,updated_at)
	SELECT gen_random_uuid(),id,'requested',$2,now(),now() FROM inserted`, prefix, user); err != nil {
		t.Fatal(err)
	}
	if _, err := s.List(ctx, user, requests.ListQuery{Q: prefix}); !errors.Is(err, requests.ErrQueryTooLarge) {
		t.Fatalf("oversized result was silently truncated: %v", err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE requests SET status='approved' WHERE id=(SELECT id FROM requests WHERE requested_by=$1 ORDER BY id LIMIT 1)`, user); err != nil {
		t.Fatal(err)
	}
	page, err := s.List(ctx, user, requests.ListQuery{Q: prefix, Status: domain.RequestRequested})
	if err != nil || len(page.Items) != 100 || page.NextCursor == nil {
		t.Fatalf("10000-item boundary/default page size: count=%d next=%v err=%v", len(page.Items), page.NextCursor != nil, err)
	}
	var size int
	if err := s.pool.QueryRow(ctx, `SELECT cardinality(request_ids) FROM request_list_snapshots WHERE user_id=$1`, user).Scan(&size); err != nil || size != 10000 {
		t.Fatalf("snapshot size=%d err=%v", size, err)
	}
}

func TestConcurrentRequestFirstPagesRespectSnapshotRetention(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s := openJobStore(t, ctx)
	f := newQueueFixture(t, ctx, s)
	start := make(chan struct{})
	results := make(chan error, 8)
	for range 8 {
		go func() {
			<-start
			_, err := s.List(ctx, f.user, requests.ListQuery{Q: f.prefix, Limit: 1})
			results <- err
		}()
	}
	close(start)
	for range 8 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM request_list_snapshots WHERE user_id=$1`, f.user).Scan(&count); err != nil || count != 5 {
		t.Fatalf("concurrent snapshot count=%d err=%v", count, err)
	}
}

func requestIDs(items []domain.Request) []string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}
