package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/auth"
	"github.com/evilperson1337/requestarr/internal/buildinfo"
)

type limitClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *limitClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *limitClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// switchableAuthRepo answers every session as whichever user is current.
type switchableAuthRepo struct {
	csrfAuthRepo
	mu   *sync.Mutex
	user *auth.User
}

func (f switchableAuthRepo) GetSession(context.Context, []byte, time.Time) (auth.User, []byte, time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return *f.user, auth.Hash("test-secret", "csrf-token"), time.Now().Add(time.Hour), nil
}

// limitedSearch builds the router with a 3-search burst refilling one per
// 2 seconds, on a clock the test moves.
func limitedSearch(t *testing.T, f *fakeSearcher) (h http.Handler, clock *limitClock, as func(id string)) {
	t.Helper()
	clock = &limitClock{now: time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)}
	user := &auth.User{ID: "u1", Role: "requester"}
	var mu sync.Mutex
	repo := switchableAuthRepo{csrfAuthRepo{statusAuthRepo{}}, &mu, user}
	d := testDeps(testLogger(), buildinfo.Info{}, fakeReadiness{}, nil, nil, nil,
		&auth.Handler{Repo: repo, Secret: "test-secret", PublicURL: "http://example.test"})
	d.Search = movieTVService(f)
	d.SearchLimiter = NewSearchLimiter(3, 2*time.Second, clock.Now)
	h, err := NewRouter(d)
	if err != nil {
		t.Fatal(err)
	}
	return h, clock, func(id string) { mu.Lock(); user.ID = id; mu.Unlock() }
}

// A typeahead person at 300 to 400 ms per keystroke sends a search per
// keystroke (the browser debounce is 300 ms). The default budget must never
// limit that, however long they type, but must still stop a script.
func TestDefaultSearchLimitNeverLimitsATypistButStopsABurst(t *testing.T) {
	for _, gap := range []time.Duration{300 * time.Millisecond, 350 * time.Millisecond, 400 * time.Millisecond} {
		clock := &limitClock{now: time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)}
		l := NewSearchLimiter(searchLimitBurst, searchLimitRefill, clock.Now)
		for at := time.Duration(0); at < 20*time.Second; at += gap {
			if ok, _ := l.Allow("typist"); !ok {
				t.Fatalf("typist at %v per keystroke limited after %v", gap, at)
			}
			clock.Advance(gap)
		}
	}
	clock := &limitClock{now: time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)}
	l := NewSearchLimiter(searchLimitBurst, searchLimitRefill, clock.Now)
	limited := 0
	for i := 0; i < 50; i++ {
		if ok, _ := l.Allow("script"); !ok {
			limited++
		}
		clock.Advance(20 * time.Millisecond) // 50 requests in one second
	}
	if limited == 0 {
		t.Fatal("50 searches in one second were not limited")
	}
}

func TestSearchOverTheLimitAnswers429WithARetryHint(t *testing.T) {
	f := &fakeSearcher{results: duneResults}
	h, _, _ := limitedSearch(t, f)
	for i := 0; i < 3; i++ {
		if w := searchGet(h, "/search?q=dune"+strconv.Itoa(i)+"&type=movie_tv", true); w.Code != 200 {
			t.Fatalf("search %d: %d", i, w.Code)
		}
	}
	w := searchGet(h, "/search?q=dune3&type=movie_tv", true)
	if w.Code != 429 || !strings.Contains(w.Body.String(), `"search_rate_limited"`) || w.Header().Get("Retry-After") != "2" {
		t.Fatalf("%d %v %s", w.Code, w.Header(), w.Body.String())
	}
	if f.callCount() != 3 {
		t.Fatalf("provider called %d times; a limited search must not reach it", f.callCount())
	}
}

func TestSearchLimitIsPerUserAndRecoversAfterTheWait(t *testing.T) {
	f := &fakeSearcher{results: duneResults}
	h, clock, as := limitedSearch(t, f)
	for i := 0; i < 3; i++ {
		searchGet(h, "/search?q=dune"+strconv.Itoa(i)+"&type=movie_tv", true)
	}
	if w := searchGet(h, "/search?q=dune9&type=movie_tv", true); w.Code != 429 {
		t.Fatalf("first user over the limit: %d", w.Code)
	}
	as("u2")
	if w := searchGet(h, "/search?q=dune9&type=movie_tv", true); w.Code != 200 {
		t.Fatalf("another user must not be blocked: %d", w.Code)
	}
	as("u1")
	clock.Advance(1999 * time.Millisecond)
	if w := searchGet(h, "/search?q=dune9&type=movie_tv", true); w.Code != 429 {
		t.Fatalf("just before the hint: %d", w.Code)
	}
	clock.Advance(time.Millisecond)
	if w := searchGet(h, "/search?q=dune9&type=movie_tv", true); w.Code != 200 {
		t.Fatalf("after the hinted wait: %d", w.Code)
	}
}

// Decision: a repeat served from the fresh cache (or joined to an identical
// in-flight query) still spends a token. The limit protects the server and caps
// one client's request rate; it is not a count of provider calls.
func TestSearchCachedRepeatsStillCountTowardTheLimit(t *testing.T) {
	f := &fakeSearcher{results: duneResults}
	h, _, _ := limitedSearch(t, f)
	for i := 0; i < 3; i++ {
		if w := searchGet(h, "/search?q=dune&type=movie_tv", true); w.Code != 200 {
			t.Fatalf("repeat %d: %d", i, w.Code)
		}
	}
	if f.callCount() != 1 {
		t.Fatalf("provider called %d times for one repeated query", f.callCount())
	}
	if w := searchGet(h, "/search?q=dune&type=movie_tv", true); w.Code != 429 {
		t.Fatalf("a fourth repeat inside the burst window: %d", w.Code)
	}
}

func TestRejectedSearchesCostNothing(t *testing.T) {
	f := &fakeSearcher{results: duneResults}
	h, _, _ := limitedSearch(t, f)
	for i := 0; i < 10; i++ {
		if w := searchGet(h, "/search?q=d&type=movie_tv", true); w.Code != 400 {
			t.Fatalf("%d", w.Code)
		}
	}
	if w := searchGet(h, "/search?q=dune&type=movie_tv", true); w.Code != 200 {
		t.Fatalf("invalid requests spent the budget: %d", w.Code)
	}
}

func TestSearchLimitDoesNotTouchSearchTypesOrResolve(t *testing.T) {
	f := &fakeSearcher{results: duneResults}
	h, _, _ := limitedSearch(t, f)
	for i := 0; i < 4; i++ {
		searchGet(h, "/search?q=dune&type=movie_tv", true)
	}
	if w := searchGet(h, "/search/types", true); w.Code != 200 {
		t.Fatalf("types: %d", w.Code)
	}
	r := httptest.NewRequest(http.MethodPost, "/api/v1/resolve", strings.NewReader(`{"url":"https://www.themoviedb.org/movie/438631"}`))
	r.AddCookie(&http.Cookie{Name: "requestarr_session", Value: "valid"})
	r.Header.Set("X-CSRF-Token", "csrf-token")
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code == 429 {
		t.Fatalf("resolve was limited: %s", w.Body.String())
	}
}
