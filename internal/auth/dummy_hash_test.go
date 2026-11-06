package auth

import (
	"net/http"
	"testing"
)

// S6: the dummy hash used for unknown usernames is made when the handler is
// registered at startup, so the first unknown-username request does not pay
// for (and so does not reveal itself by) an extra hash.
func TestTheDummyHashIsMadeAtStartupNotOnTheFirstUnknownUsername(t *testing.T) {
	f := newFixture(t)
	h := &countingHasher{Hasher: f.hasher.Hasher}
	fresh := &Handler{Repo: f.store, Local: f.store, Hasher: h, Clock: f.clock.Now, Secret: testSecret, PublicURL: testPublicURL, Secure: true, Sleep: f.sleep}
	fresh.Register(http.NewServeMux())
	if h.hashes != 1 {
		t.Fatalf("Register made %d hashes, want the one dummy hash", h.hashes)
	}
	h.reset()
	mux := http.NewServeMux()
	fresh.Register(mux) // already made: no second hash
	if h.hashes != 0 {
		t.Fatalf("a second Register made %d hashes", h.hashes)
	}
	f.mux = mux
	w := f.login("nobody", "whatever password")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", w.Code)
	}
	if h.hashes != 0 || h.verifies != 1 {
		t.Fatalf("unknown username: hashes=%d verifies=%d, want 0 and exactly 1 (same work as a wrong password)", h.hashes, h.verifies)
	}
}
