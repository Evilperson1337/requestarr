package postgres

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestOpenFailureIsBoundedAndSanitized(t *testing.T) {
	secret := "database-secret"
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	_, err := Open(ctx, "postgres://requestarr:"+secret+"@203.0.113.1:1/requestarr?sslmode=disable")
	if err == nil {
		t.Fatal("Open() succeeded against unavailable PostgreSQL")
	}
	if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "postgres://") {
		t.Fatalf("Open() error exposed connection URL: %v", err)
	}
}

func TestNilPoolIsNotReady(t *testing.T) {
	var pool *Pool
	if err := pool.Ready(context.Background()); err == nil {
		t.Fatal("Ready() succeeded for nil pool")
	}
}
