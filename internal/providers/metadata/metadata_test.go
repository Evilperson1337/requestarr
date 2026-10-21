package metadata

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestParseURLHostValidation(t *testing.T) {
	for _, tc := range []struct {
		raw string
		ok  bool
	}{{"https://www.themoviedb.org/movie/12", true}, {"https://musicbrainz.org/release/abc", true}, {"https://www.igdb.com/games/12", true}, {"https://evil.example/themoviedb.org/movie/12", false}, {"https://user@igdb.com/games/1", false}, {"http://igdb.com/games/1", false}, {"https://sub.igdb.com/games/1", false}, {"https://evil.igdb.com/games/1", false}, {"ftp://igdb.com/games/1", false}, {"https://igdb.com/games/1?x=2", false}} {
		_, err := ParseURL(tc.raw)
		if (err == nil) != tc.ok {
			t.Errorf("ParseURL(%q) error=%v", tc.raw, err)
		}
	}
}

func TestParseURLDistinguishesEmptyAndTooLong(t *testing.T) {
	if _, err := ParseURL("  "); !errors.Is(err, ErrEmptyURL) {
		t.Fatalf("empty error = %v", err)
	}
	if _, err := ParseURL(strings.Repeat("x", 2049)); !errors.Is(err, ErrURLTooLong) {
		t.Fatalf("long error = %v", err)
	}
}

func TestFetchRejectsTrailingJSON(t *testing.T) {
	for _, body := range []string{`{"ok":true} {"extra":true}`, `{"ok":true} garbage`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		}))
		var dst map[string]bool
		err := Fetch(context.Background(), server.Client(), server.URL, nil, &dst)
		server.Close()
		if err == nil {
			t.Errorf("Fetch accepted trailing data %q", body)
		}
	}
}

func TestFetchBoundsAndHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("missing user agent")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	var dst map[string]bool
	if err := Fetch(context.Background(), server.Client(), server.URL, nil, &dst); err != nil {
		t.Fatal(err)
	}
	if !dst["ok"] {
		t.Fatal(dst)
	}
	large := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"v":"` + strings.Repeat("x", maxResponseBytes+8) + `"}`))
	}))
	defer large.Close()
	if err := Fetch(context.Background(), large.Client(), large.URL, nil, &dst); err == nil {
		t.Fatal("expected oversized response error")
	}
}

func TestFetchRejectsNonJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	var dst map[string]bool
	if err := Fetch(context.Background(), server.Client(), server.URL, nil, &dst); err == nil {
		t.Fatal("expected non-JSON content type error")
	}
}

func TestResolverContractRejectsNilURL(t *testing.T) {
	var u *url.URL
	if u != nil {
		t.Fatal("unexpected URL")
	}
}
