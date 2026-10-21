package apprise

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/example/requestarr/internal/notifications"
)

func TestSendPostsJSONToConfiguredGlobalKey(t *testing.T) {
	var path, userAgent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		userAgent = r.UserAgent()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	c := New(srv.URL, "key")
	c.HTTP = srv.Client()
	if err := c.Send(context.Background(), notifications.Message{Title: "Available", Body: "Ready"}); err != nil {
		t.Fatal(err)
	}
	if path != "/notify/key" || userAgent == "" {
		t.Fatalf("path=%q user-agent=%q", path, userAgent)
	}
}

func TestSendRejectsUnsafeServiceURL(t *testing.T) {
	c := New("http://example.org", "key")
	c.HTTP = http.DefaultClient
	if err := c.Send(context.Background(), notifications.Message{Title: "Available"}); err == nil {
		t.Fatal("expected unsafe URL rejection")
	}
}

func TestSendDoesNotFollowRedirectContainingConfigKey(t *testing.T) {
	var destinationHits atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		destinationHits.Add(1)
	}))
	defer destination.Close()

	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL+"/leaked", http.StatusFound)
	}))
	defer source.Close()

	client := New(source.URL, "secret-config-key")
	err := client.Send(context.Background(), notifications.Message{Title: "t", Body: "b"})
	if err == nil {
		t.Fatal("expected redirect response to be rejected")
	}
	if got := destinationHits.Load(); got != 0 {
		t.Fatalf("redirect destination received %d requests", got)
	}
}
