package postgres

import (
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// recorder stands in for time: it records every pause instead of waiting.
type recorder struct{ pauses []time.Duration }

func (r *recorder) sleep(_ context.Context, d time.Duration) error {
	r.pauses = append(r.pauses, d)
	return nil
}

func policy(r *recorder) RetryPolicy {
	return RetryPolicy{Attempts: 3, Wait: 5 * time.Second, sleep: r.sleep}
}

var notReady = &connectError{message: "database is unreachable: nothing accepted the connection at db:5432", transient: true}

// A database that is not up yet (a container still starting, the order two
// containers boot in) is worth a short wait; the operator asked for three
// tries, five seconds apart, and no pause after the last one.
func TestRetryStopsAfterThreeAttemptsFiveSecondsApart(t *testing.T) {
	r := &recorder{}
	tries := 0
	_, err := retry(context.Background(), policy(r), func(context.Context) (int, error) { tries++; return 0, notReady })
	if err == nil {
		t.Fatal("expected a failure after the last attempt")
	}
	if tries != 3 {
		t.Fatalf("want 3 attempts, got %d", tries)
	}
	if len(r.pauses) != 2 || r.pauses[0] != 5*time.Second || r.pauses[1] != 5*time.Second {
		t.Fatalf("want two 5s pauses and none after the last attempt, got %v", r.pauses)
	}
	for _, want := range []string{"nothing accepted the connection", "gave up after 3 attempts", "5s apart"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("message %q should contain %q", err.Error(), want)
		}
	}
}

func TestRetrySucceedsWhenTheDatabaseComesUp(t *testing.T) {
	r := &recorder{}
	tries := 0
	got, err := retry(context.Background(), policy(r), func(context.Context) (string, error) {
		tries++
		if tries < 3 {
			return "", notReady
		}
		return "connected", nil
	})
	if err != nil || got != "connected" || tries != 3 || len(r.pauses) != 2 {
		t.Fatalf("got %q err=%v tries=%d pauses=%v", got, err, tries, r.pauses)
	}
}

func TestNothingIsWaitedForWhenTheFirstAttemptWorks(t *testing.T) {
	r := &recorder{}
	if _, err := retry(context.Background(), policy(r), func(context.Context) (int, error) { return 1, nil }); err != nil || len(r.pauses) != 0 {
		t.Fatalf("err=%v pauses=%v", err, r.pauses)
	}
}

// Waiting cannot fix a wrong password or a missing database, so those fail at
// once, with the same message as before and no extra delay.
func TestACauseWaitingCannotFixIsNotRetried(t *testing.T) {
	for name, err := range map[string]error{
		"rejected login":   unreachable(&pgconn.PgError{Code: "28P01"}, "db", 5432, "requestarr"),
		"missing database": unreachable(&pgconn.PgError{Code: "3D000"}, "db", 5432, "requestarr"),
		"tls":              unreachable(errors.New("tls error"), "db", 5432, "requestarr"),
	} {
		r := &recorder{}
		tries := 0
		_, got := retry(context.Background(), policy(r), func(context.Context) (int, error) { tries++; return 0, err })
		if tries != 1 || len(r.pauses) != 0 {
			t.Fatalf("%s: tried %d times, paused %v", name, tries, r.pauses)
		}
		if got.Error() != err.Error() || strings.Contains(got.Error(), "gave up") {
			t.Fatalf("%s: message changed to %q", name, got.Error())
		}
	}
}

func TestWhichCausesAreWorthWaitingFor(t *testing.T) {
	waits := map[string]error{
		"refused":          &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED},
		"does not resolve": &net.DNSError{Err: "no such host", Name: "db", IsNotFound: true},
		"timed out":        context.DeadlineExceeded,
		"starting up":      &pgconn.PgError{Code: "57P03"},
		"no free slots":    &pgconn.PgError{Code: "53300"},
		"unknown":          errors.New("connection reset by peer"),
	}
	for name, cause := range waits {
		if !IsTransient(unreachable(cause, "db", 5432, "requestarr")) {
			t.Errorf("%s should be retried", name)
		}
	}
	for name, cause := range map[string]error{
		"rejected login":   &pgconn.PgError{Code: "28000"},
		"missing database": &pgconn.PgError{Code: "3D000"},
		"other refusal":    &pgconn.PgError{Code: "42501"},
		"tls":              errors.New("tls: handshake failure"),
	} {
		if IsTransient(unreachable(cause, "db", 5432, "requestarr")) {
			t.Errorf("%s should fail at once", name)
		}
	}
}

// Stopping the server while it waits must not take another five seconds.
func TestShuttingDownWhileWaitingStopsAtOnce(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	tries := 0
	start := time.Now()
	_, err := retry(ctx, RetryPolicy{Attempts: 3, Wait: 30 * time.Second}, func(context.Context) (int, error) {
		tries++
		cancel()
		return 0, notReady
	})
	if !errors.Is(err, context.Canceled) || tries != 1 || time.Since(start) > 2*time.Second {
		t.Fatalf("err=%v tries=%d took %v", err, tries, time.Since(start))
	}
}

// The whole path against a port nothing listens on: the real classification,
// three tries, and the operator told after each failed one.
func TestOpenWithRetryReportsEachFailedAttempt(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("setup failed (%T)", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	var seen []int
	p := RetryPolicy{Attempts: 3, Wait: 10 * time.Millisecond, OnRetry: func(attempt, attempts int, _ time.Duration, err error) {
		if attempts != 3 || !strings.Contains(err.Error(), "nothing accepted the connection") {
			t.Errorf("OnRetry got attempt=%d of %d, %q", attempt, attempts, err.Error())
		}
		seen = append(seen, attempt)
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err = OpenWithRetry(ctx, "postgres://app:CANARY-PASSWORD@127.0.0.1:"+itoa(port)+"/requestarr", p)
	if err == nil || len(seen) != 2 || seen[0] != 1 || seen[1] != 2 {
		t.Fatalf("err=%v retries reported for attempts %v", err, seen)
	}
	if !strings.Contains(err.Error(), "gave up after 3 attempts") || strings.Contains(err.Error(), "CANARY-PASSWORD") {
		t.Fatalf("unexpected final message %q", err.Error())
	}
}

func itoa(n int) string { return strconv.Itoa(n) }
