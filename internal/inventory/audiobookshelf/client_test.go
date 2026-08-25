package audiobookshelf

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"git.example.com/evilperson1337/Requestarr/internal/domain"
	"git.example.com/evilperson1337/Requestarr/internal/inventory"
)

type fixedToken string

func (t fixedToken) BearerToken(context.Context) (string, error) { return string(t), nil }

type failingToken struct{}

func (failingToken) BearerToken(context.Context) (string, error) {
	return "", errors.New("token provider failure")
}

type fixedLibraries []string

func (l fixedLibraries) CompleteReaderRequirements(context.Context) (inventory.CompleteReaderRequirements, error) {
	return inventory.CompleteReaderRequirements{SourceID: "source", SelectedLibraries: append([]string(nil), l...), EnabledMediaTypes: []domain.MediaType{domain.MediaAudiobook}}, nil
}

type fixedResolver []netip.Addr

func (r fixedResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return append([]netip.Addr(nil), r...), nil
}

type forwardingDialer struct{ target string }

func (d forwardingDialer) DialContext(ctx context.Context, network, _ string) (net.Conn, error) {
	return (&net.Dialer{}).DialContext(ctx, network, d.target)
}

type recordingDialer struct {
	target string
	mu     sync.Mutex
	addrs  []string
}

type blockingDialer struct{}

func (blockingDialer) DialContext(ctx context.Context, _, _ string) (net.Conn, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (d *recordingDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	d.mu.Lock()
	d.addrs = append(d.addrs, address)
	d.mu.Unlock()
	return (&net.Dialer{}).DialContext(ctx, network, d.target)
}

func (d *recordingDialer) addresses() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.addrs...)
}

func TestClientReadCompleteSyntheticFixture(t *testing.T) {
	libraries := fixture(t, "libraries.json")
	items := fixture(t, "items_page_0.json")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer synthetic-token" {
			t.Fatalf("Authorization = %q", got)
		}
		if got := r.Header.Get("User-Agent"); got != userAgent {
			t.Fatalf("User-Agent = %q", got)
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		switch r.URL.Path {
		case "/api/libraries":
			_, _ = w.Write(libraries)
		case "/api/libraries/fiction/items":
			if r.URL.Query().Get("limit") != "100" || r.URL.Query().Get("page") != "0" {
				t.Fatalf("query = %q", r.URL.RawQuery)
			}
			_, _ = w.Write(items)
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	client := testClient(t, server.URL, fixedLibraries{"fiction"})
	snapshot, err := client.Read(context.Background(), defaultRequirements())
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if !snapshot.Complete || len(snapshot.Coverage) != 1 || len(snapshot.Items) != 2 {
		t.Fatalf("result = %+v", snapshot)
	}
	if snapshot.Items[0].Title != "Synthetic Title" {
		t.Fatalf("first item = %+v", snapshot.Items[0])
	}
	if len(snapshot.Items[0].ExternalIDs) != 0 || snapshot.Items[0].ExternalURL != "" {
		t.Fatalf("reader inferred enrichment evidence: %+v", snapshot.Items[0])
	}
}

func TestClientReadRejectsIncompleteAndDuplicatePages(t *testing.T) {
	tests := []struct{ name, body string }{
		{name: "short page", body: `{"results":[{"id":"book-001","media":{"metadata":{"title":"Title"}}}],"total":2,"limit":100,"page":0}`},
		{name: "duplicate key", body: `{"results":[],"results":[],"total":0,"limit":100,"page":0}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/api/libraries" {
					_, _ = w.Write([]byte(`[{"id":"fiction","name":"Fiction","mediaType":"book"}]`))
					return
				}
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()
			_, err := testClient(t, server.URL, fixedLibraries{"fiction"}).Read(context.Background(), defaultRequirements())
			if !errors.Is(err, ErrRead) {
				t.Fatalf("Read() error = %v", err)
			}
		})
	}
}

func TestClientReadProvesAllPages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/libraries" {
			_, _ = w.Write([]byte(`[{"id":"fiction","name":"Fiction","mediaType":"book"}]`))
			return
		}
		page := r.URL.Query().Get("page")
		if page == "0" {
			_, _ = w.Write([]byte(`{"results":[` + syntheticItems(0, 100) + `],"total":101,"limit":100,"page":0}`))
			return
		}
		if page == "1" {
			_, _ = w.Write([]byte(`{"results":[` + syntheticItems(100, 1) + `],"total":101,"limit":100,"page":1}`))
			return
		}
		t.Fatalf("unexpected page %q", page)
	}))
	defer server.Close()
	snapshot, err := testClient(t, server.URL, fixedLibraries{"fiction"}).Read(context.Background(), defaultRequirements())
	if err != nil || len(snapshot.Items) != 101 {
		t.Fatalf("Read() snapshot items=%d, error=%v", len(snapshot.Items), err)
	}
}

func TestClientReadCompleteCoverageAcrossSelectedLibraries(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/libraries":
			_, _ = w.Write([]byte(`[{"id":"one","name":"One","mediaType":"book"},{"id":"two","name":"Two","mediaType":"book"}]`))
		case "/api/libraries/one/items":
			_, _ = w.Write([]byte(`{"results":[{"id":"same","media":{"metadata":{"title":"One"}}}],"total":1,"limit":100,"page":0}`))
		case "/api/libraries/two/items":
			_, _ = w.Write([]byte(`{"results":[],"total":0,"limit":100,"page":0}`))
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	result, err := testClient(t, server.URL, fixedLibraries{"one", "two"}).Read(context.Background(), audiobookRequirements("one", "two"))
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if len(result.Coverage) != 2 || !hasCoverage(result, "one") || !hasCoverage(result, "two") || len(result.Items) != 1 || result.Items[0].RemoteLibraryID != "one" || result.Items[0].SourceItemID != "same" {
		t.Fatalf("result = %+v", result)
	}
}

func TestClientReadAllowsSameRawIDInDifferentLibraries(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/libraries" {
			_, _ = w.Write([]byte(`[{"id":"one","name":"One","mediaType":"book"},{"id":"two","name":"Two","mediaType":"book"}]`))
			return
		}
		_, _ = w.Write([]byte(`{"results":[{"id":"same","media":{"metadata":{"title":"Title"}}}],"total":1,"limit":100,"page":0}`))
	}))
	defer server.Close()
	result, err := testClient(t, server.URL, fixedLibraries{"one", "two"}).Read(context.Background(), audiobookRequirements("one", "two"))
	if err != nil || len(result.Items) != 2 || result.Items[0].RemoteLibraryID == result.Items[1].RemoteLibraryID {
		t.Fatalf("result=%+v error=%v", result, err)
	}
}

func TestClientReadRejectsInvalidScopeAndDoesNotReturnPartialResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/libraries" {
			_, _ = w.Write([]byte(`[{"id":"one","name":"One","mediaType":"book"},{"id":"two","name":"Two","mediaType":"book"}]`))
			return
		}
		if r.URL.Path == "/api/libraries/one/items" {
			_, _ = w.Write([]byte(`{"results":[],"total":0,"limit":100,"page":0}`))
			return
		}
		http.Error(w, "failure", http.StatusInternalServerError)
	}))
	defer server.Close()
	client := testClient(t, server.URL, fixedLibraries{"one", "two"})
	result, err := client.Read(context.Background(), audiobookRequirements("one", "two"))
	if !errors.Is(err, ErrRead) || !emptyResult(result) {
		t.Fatalf("result=%+v error=%v", result, err)
	}
	result, err = client.Read(context.Background(), inventory.CompleteReaderRequirements{SourceID: "source"})
	if !errors.Is(err, ErrRead) || !emptyResult(result) {
		t.Fatalf("result=%+v error=%v", result, err)
	}
}

func TestClientReadRejectsSelectedNonBookLibraryBeforeEnumeration(t *testing.T) {
	itemsRequested := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/libraries" {
			_, _ = w.Write([]byte(`[{"id":"fiction","name":"Fiction","mediaType":"book"},{"id":"video","name":"Video","mediaType":"video"}]`))
			return
		}
		itemsRequested = true
		_, _ = w.Write([]byte(`{"results":[],"total":0,"limit":100,"page":0}`))
	}))
	defer server.Close()

	result, err := testClient(t, server.URL, fixedLibraries{"fiction", "video"}).Read(context.Background(), audiobookRequirements("fiction", "video"))
	if !errors.Is(err, ErrInvalidResponse) || !errors.Is(err, ErrRead) || !emptyResult(result) {
		t.Fatalf("result=%+v error=%v", result, err)
	}
	if itemsRequested {
		t.Fatal("reader enumerated a selected non-book library")
	}
}

func TestClientReadErrorTaxonomy(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		body     string
		category error
	}{
		{name: "unauthorized", status: http.StatusUnauthorized, category: ErrAuthentication},
		{name: "forbidden", status: http.StatusForbidden, category: ErrAuthentication},
		{name: "server unavailable", status: http.StatusServiceUnavailable, category: ErrUnavailable},
		{name: "malformed response", status: http.StatusOK, body: `{`, category: ErrInvalidResponse},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			result, err := testClient(t, server.URL, fixedLibraries{"fiction"}).Read(context.Background(), defaultRequirements())
			if !errors.Is(err, ErrRead) || !errors.Is(err, tt.category) || !emptyResult(result) {
				t.Fatalf("result=%+v error=%v", result, err)
			}
		})
	}
}

func TestClientReadClassifiesTokenTransportAndCancellationFailures(t *testing.T) {
	t.Run("token failure", func(t *testing.T) {
		client := testClient(t, "http://127.0.0.1:1", fixedLibraries{"fiction"})
		client.tokens = failingToken{}
		result, err := client.Read(context.Background(), defaultRequirements())
		if !errors.Is(err, ErrAuthentication) || !emptyResult(result) {
			t.Fatalf("result=%+v error=%v", result, err)
		}
	})
	t.Run("transport failure", func(t *testing.T) {
		client := testClient(t, "http://127.0.0.1:1", fixedLibraries{"fiction"})
		result, err := client.Read(context.Background(), defaultRequirements())
		if !errors.Is(err, ErrUnavailable) || !emptyResult(result) {
			t.Fatalf("result=%+v error=%v", result, err)
		}
	})
	t.Run("caller cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		result, err := testClient(t, "http://127.0.0.1:1", fixedLibraries{"fiction"}).Read(ctx, defaultRequirements())
		if !errors.Is(err, ErrCanceled) || !errors.Is(err, context.Canceled) || !emptyResult(result) {
			t.Fatalf("result=%+v error=%v", result, err)
		}
	})
}

func TestClientReadRejectsUnselectedLibraryAndDeniedDestination(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"fiction","name":"Fiction","mediaType":"book"}]`))
	}))
	defer server.Close()
	_, err := testClient(t, server.URL, fixedLibraries{"missing"}).Read(context.Background(), defaultRequirements())
	if !errors.Is(err, ErrRead) || strings.Contains(err.Error(), "missing") {
		t.Fatalf("unselected error = %v", err)
	}
	base, _ := url.Parse(server.URL)
	client, err := NewClient(Options{BaseURL: base.Scheme + "://abs.example", SourceID: "source", TokenProvider: fixedToken("token"), Resolver: fixedResolver{netip.MustParseAddr("127.0.0.1")}, Dialer: forwardingDialer{target: base.Host}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Read(context.Background(), defaultRequirements())
	if !errors.Is(err, ErrRead) {
		t.Fatalf("denied destination error = %v", err)
	}
}

func TestClientReadTransportSecurity(t *testing.T) {
	const token = "secret-bearer-token"
	redirected := false
	redirectTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirected = true
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("redirect authorization = %q", got)
		}
	}))
	defer redirectTarget.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Host; !strings.HasPrefix(got, "abs.example:") {
			t.Errorf("Host = %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+token {
			t.Errorf("Authorization = %q", got)
		}
		http.Redirect(w, r, redirectTarget.URL+"/redirect-secret", http.StatusFound)
	}))
	defer server.Close()

	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	dialer := &recordingDialer{target: base.Host}
	client, err := NewClient(Options{
		BaseURL:       "http://abs.example:" + base.Port() + "/configured-secret-path",
		SourceID:      "source",
		TokenProvider: fixedToken(token),
		Resolver:      fixedResolver{netip.MustParseAddr("127.0.0.1")},
		Dialer:        dialer,
		AllowedCIDRs:  []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Read(context.Background(), defaultRequirements())
	if !errors.Is(err, ErrRead) {
		t.Fatalf("Read() error = %v", err)
	}
	if redirected {
		t.Fatal("client followed redirect")
	}
	addresses := dialer.addresses()
	if len(addresses) != 1 || addresses[0] != net.JoinHostPort("127.0.0.1", base.Port()) {
		t.Fatalf("dial addresses = %q", addresses)
	}
	transport, ok := client.httpClient([]netip.Addr{netip.MustParseAddr("127.0.0.1")}).Transport.(*http.Transport)
	if !ok {
		t.Fatal("unexpected transport type")
	}
	if transport.Proxy != nil {
		t.Fatal("transport proxy must be disabled")
	}
}

func TestClientReadRejectsUnsafeResponsesWithoutLeakingSecrets(t *testing.T) {
	oversized := strings.Repeat("x", maxResponseBytes+1)
	tests := []struct {
		name        string
		contentType string
		body        string
	}{
		{name: "non JSON suffix", contentType: "application/jsonp", body: `[]`},
		{name: "invalid media type", contentType: `application/json; charset="unterminated`, body: `[]`},
		{name: "oversized", contentType: "application/json", body: oversized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tt.contentType)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()
			client := testClient(t, server.URL+"/private-path", fixedLibraries{"fiction"})
			_, err := client.Read(context.Background(), defaultRequirements())
			if !errors.Is(err, ErrRead) {
				t.Fatalf("Read() error = %v", err)
			}
			for _, secret := range []string{"synthetic-token", "abs.example", "private-path", oversized} {
				if strings.Contains(err.Error(), secret) {
					t.Fatalf("error leaked %q: %v", secret, err)
				}
			}
		})
	}
}

func TestClientResolveRejectsMixedAnswersAndReadHonorsCancellation(t *testing.T) {
	client, err := NewClient(Options{
		BaseURL:       "http://abs.example",
		SourceID:      "source",
		TokenProvider: fixedToken("token"),
		Resolver:      fixedResolver{netip.MustParseAddr("127.0.0.1"), netip.MustParseAddr("192.0.2.1")},
		AllowedCIDRs:  []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.resolve(context.Background()); !errors.Is(err, ErrRead) {
		t.Fatalf("resolve error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.Read(ctx, defaultRequirements()); !errors.Is(err, ErrRead) {
		t.Fatalf("Read() error = %v", err)
	}
}

func TestClientReadHonorsCallerDeadline(t *testing.T) {
	client, err := NewClient(Options{
		BaseURL:       "http://abs.example",
		SourceID:      "source",
		TokenProvider: fixedToken("token"),
		Resolver:      fixedResolver{netip.MustParseAddr("127.0.0.1")},
		Dialer:        blockingDialer{},
		AllowedCIDRs:  []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err = client.Read(ctx, defaultRequirements())
	if !errors.Is(err, ErrRead) || !errors.Is(err, ErrCanceled) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Read() error = %v", err)
	}
}

func testClient(t *testing.T, rawURL string, selected fixedLibraries) *Client {
	t.Helper()
	base, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(Options{BaseURL: "http://abs.example", SourceID: "source", TokenProvider: fixedToken("synthetic-token"), Resolver: fixedResolver{netip.MustParseAddr("127.0.0.1")}, Dialer: forwardingDialer{target: base.Host}, AllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}, Now: func() time.Time { return time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func defaultRequirements() inventory.CompleteReaderRequirements {
	return audiobookRequirements("fiction")
}

func audiobookRequirements(libraries ...string) inventory.CompleteReaderRequirements {
	return inventory.CompleteReaderRequirements{SourceID: "source", SelectedLibraries: libraries, EnabledMediaTypes: []domain.MediaType{domain.MediaAudiobook}}
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	value, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func syntheticItems(start, count int) string {
	var result strings.Builder
	for i := start; i < start+count; i++ {
		if i > start {
			result.WriteByte(',')
		}
		fmt.Fprintf(&result, `{"id":"synthetic-%03d","media":{"metadata":{"title":"Title %03d"}}}`, i, i)
	}
	return result.String()
}

func hasCoverage(result inventory.CompleteReaderResult, libraryID string) bool {
	for _, coverage := range result.Coverage {
		if coverage.RemoteLibraryID == libraryID && coverage.MediaType == domain.MediaAudiobook && coverage.Status == inventory.CoverageVerifiedComplete {
			return true
		}
	}
	return false
}

func emptyResult(result inventory.CompleteReaderResult) bool {
	return result.SourceID == "" && result.CompletedAt.IsZero() && !result.Complete && len(result.Coverage) == 0 && len(result.Items) == 0
}
