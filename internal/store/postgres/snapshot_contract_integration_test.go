//go:build integration

package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/store/snapshottest"
)

// pgBackend lets the PostgreSQL store run the same snapshot-persistence
// contract as the in-memory adapter.
type pgBackend struct {
	*Store
	ctx context.Context
}

func (b pgBackend) AddMovie(tb testing.TB, tmdbID string) string {
	tb.Helper()
	id := newUUID()
	if _, err := b.pool.Exec(b.ctx, `INSERT INTO catalog_items(id,media_type,provider,provider_id,title,artwork_kind,external_url,metadata,created_at,updated_at) VALUES($1,'movie','tmdb',$2,'Contract movie','poster','https://example.test','{}',now(),now())`, id, tmdbID); err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { _, _ = b.pool.Exec(context.Background(), `DELETE FROM catalog_items WHERE id=$1`, id) })
	return id
}

func (b pgBackend) OpenRequest(tb testing.TB, catalogID string) string {
	tb.Helper()
	t, ok := tb.(*testing.T)
	if !ok {
		tb.Fatal("OpenRequest needs a *testing.T")
	}
	requester := integrationUser(t, b.ctx, b.Store)
	req, _, err := b.CreateOrGet(b.ctx, domain.Request{CatalogItemID: catalogID, Status: domain.RequestRequested, RequestedBy: requester}, domain.RequestSubscription{UserID: requester, Source: domain.SubscriptionRequester})
	if err != nil {
		tb.Fatal(err)
	}
	return req.ID
}

func (b pgBackend) Availability(tb testing.TB, catalogID string) domain.Availability {
	tb.Helper()
	var availability string
	if err := b.pool.QueryRow(b.ctx, `SELECT availability FROM catalog_items WHERE id=$1`, catalogID).Scan(&availability); err != nil {
		tb.Fatal(err)
	}
	return domain.Availability(availability)
}

func (b pgBackend) RequestStatus(tb testing.TB, requestID string) domain.RequestStatus {
	tb.Helper()
	var status string
	if err := b.pool.QueryRow(b.ctx, `SELECT status FROM requests WHERE id=$1`, requestID).Scan(&status); err != nil {
		tb.Fatal(err)
	}
	return domain.RequestStatus(status)
}

func TestPostgresHoldsTheSnapshotPersistenceContract(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	s, err := Open(ctx, testDatabaseURL(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	snapshottest.Run(t, func(*testing.T) snapshottest.Backend { return pgBackend{Store: s, ctx: ctx} })
}
