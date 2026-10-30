package metadata

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// flaky answers the first n requests with status, then a JSON body.
func flaky(n int32, status int, retryAfter string) (*httptest.Server, *int32) {
	var calls int32
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) <= n {
			if retryAfter != "" {
				w.Header().Set("Retry-After", retryAfter)
			}
			w.WriteHeader(status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})), &calls
}

func fast(t *testing.T) {
	t.Helper()
	old := retryDelay
	retryDelay = func(int, string) time.Duration { return time.Millisecond }
	t.Cleanup(func() { retryDelay = old })
}

// MusicBrainz and others answer an occasional 503 under load; one blip must
// not fail a request the user can't tell is retryable.
func TestFetchRetriesTransientProviderErrors(t *testing.T) {
	fast(t)
	for _, status := range []int{http.StatusServiceUnavailable, http.StatusTooManyRequests, http.StatusBadGateway} {
		server, calls := flaky(2, status, "")
		var out struct{ OK bool }
		if err := FetchRetrying(context.Background(), server.Client(), server.URL, nil, &out); err != nil || !out.OK {
			t.Errorf("status %d: err=%v out=%+v after %d calls", status, err, out, *calls)
		}
		server.Close()
	}
}

func TestFetchWithBodyRetriesAndResendsTheBody(t *testing.T) {
	fast(t)
	var bodies []string
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 64)
		n, _ := r.Body.Read(buf)
		bodies = append(bodies, string(buf[:n]))
		if atomic.AddInt32(&calls, 1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	var out struct{ OK bool }
	if err := FetchWithBodyRetrying(context.Background(), server.Client(), server.URL, "fields name;", nil, &out); err != nil || !out.OK {
		t.Fatalf("err=%v out=%+v", err, out)
	}
	if len(bodies) != 2 || bodies[0] != "fields name;" || bodies[1] != "fields name;" {
		t.Fatalf("bodies = %q: the retry must resend the query", bodies)
	}
}

func TestFetchGivesUpAfterBoundedAttemptsAndNeverRetriesClientErrors(t *testing.T) {
	fast(t)
	server, calls := flaky(100, http.StatusServiceUnavailable, "")
	defer server.Close()
	var out struct{ OK bool }
	if err := FetchRetrying(context.Background(), server.Client(), server.URL, nil, &out); err == nil || *calls != 3 {
		t.Fatalf("err=%v after %d calls; want an error after 3", err, *calls)
	}
	for _, status := range []int{http.StatusNotFound, http.StatusUnauthorized, http.StatusBadRequest} {
		s, c := flaky(100, status, "")
		if err := FetchRetrying(context.Background(), s.Client(), s.URL, nil, &out); err == nil || *c != 1 {
			t.Errorf("status %d retried %d times", status, *c)
		}
		s.Close()
	}
}

// The plain Fetch stays single-attempt for callers with their own budget.
func TestPlainFetchDoesNotRetry(t *testing.T) {
	fast(t)
	server, calls := flaky(100, http.StatusServiceUnavailable, "")
	defer server.Close()
	var out struct{ OK bool }
	if err := Fetch(context.Background(), server.Client(), server.URL, nil, &out); err == nil || *calls != 1 {
		t.Fatalf("err=%v after %d calls", err, *calls)
	}
	if err := FetchWithBody(context.Background(), server.Client(), server.URL, "q", nil, &out); err == nil || *calls != 2 {
		t.Fatalf("FetchWithBody: err=%v after %d total calls", err, *calls)
	}
}

func TestRetryDelayHonorsRetryAfterWithACap(t *testing.T) {
	if d := retryDelay(1, "1"); d != time.Second {
		t.Errorf("Retry-After 1 = %v", d)
	}
	if d := retryDelay(1, "3600"); d > 2*time.Second {
		t.Errorf("Retry-After 3600 = %v; it must be capped", d)
	}
	if d := retryDelay(1, "soon"); d <= 0 || d > 2*time.Second {
		t.Errorf("unparsable Retry-After = %v", d)
	}
	if retryDelay(2, "") <= retryDelay(1, "") {
		t.Error("backoff must grow")
	}
}

func TestRetryStopsWhenTheContextEnds(t *testing.T) {
	old := retryDelay
	retryDelay = func(int, string) time.Duration { return time.Hour }
	defer func() { retryDelay = old }()
	server, _ := flaky(100, http.StatusServiceUnavailable, "")
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	var out struct{ OK bool }
	if err := FetchRetrying(ctx, server.Client(), server.URL, nil, &out); err == nil || time.Since(started) > 2*time.Second {
		t.Fatalf("err=%v after %v", err, time.Since(started))
	}
}
