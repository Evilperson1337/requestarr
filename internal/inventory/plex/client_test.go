package plex

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"git.example.com/evilperson1337/Requestarr/internal/domain"
	"git.example.com/evilperson1337/Requestarr/internal/inventory"
)

type fixedToken string

func (t fixedToken) PlexToken(context.Context) (string, error) { return string(t), nil }

type fixedResolver []netip.Addr

func (r fixedResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return append([]netip.Addr(nil), r...), nil
}

type recordingResolver struct {
	addresses []netip.Addr
	calls     int
}

func (r *recordingResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	r.calls++
	return append([]netip.Addr(nil), r.addresses...), nil
}

type forwardingDialer struct{ target string }

func (d forwardingDialer) DialContext(ctx context.Context, network, _ string) (net.Conn, error) {
	return (&net.Dialer{}).DialContext(ctx, network, d.target)
}

type recordingDialer struct {
	target  string
	address string
}

func (d *recordingDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	d.address = address
	return (&net.Dialer{}).DialContext(ctx, network, d.target)
}

func TestReadMultiPageUsesSelectedSectionsAndHeaderToken(t *testing.T) {
	sections := fixture(t, "sections.json")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Plex-Token") != "synthetic-token" || r.URL.Query().Has("X-Plex-Token") || r.Header.Get("Authorization") != "" {
			t.Fatalf("unsafe authentication transport")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/library/sections":
			_, _ = w.Write(sections)
		case "/library/sections/1/all":
			if r.URL.Query().Get("X-Plex-Container-Start") == "0" {
				_, _ = w.Write([]byte(`{"MediaContainer":{"offset":0,"size":100,"totalSize":101,"Metadata":[` + metadata(0, 100) + `]}}`))
				return
			}
			if r.URL.Query().Get("X-Plex-Container-Start") == "100" {
				_, _ = w.Write([]byte(`{"MediaContainer":{"offset":100,"size":1,"totalSize":101,"Metadata":[` + metadata(100, 1) + `]}}`))
				return
			}
			t.Fatalf("unexpected pagination %q", r.URL.RawQuery)
		default:
			t.Fatalf("unexpected selected/unselected request %q", r.URL.Path)
		}
	}))
	defer server.Close()
	result, err := testClient(t, server.URL, []string{"1"}, []domain.MediaType{domain.MediaMovie}).Read(context.Background(), requirements([]string{"1"}, []domain.MediaType{domain.MediaMovie}))
	if err != nil || !result.Complete || len(result.Items) != 101 || result.Items[0].SourceItemID != "1:0" || result.Items[0].ExternalIDs["tmdb"] != "0" || !hasCoverage(result, "1", domain.MediaMovie, inventory.CoverageVerifiedComplete) {
		t.Fatalf("result=%+v error=%v", result, err)
	}
}

func TestReadAllowsZeroItems(t *testing.T) {
	zeroItems := fixture(t, "zero_items.json")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/library/sections" {
			_, _ = w.Write([]byte(`{"MediaContainer":{"Directory":[{"key":"1","title":"Movies","type":"movie"}]}}`))
			return
		}
		_, _ = w.Write(zeroItems)
	}))
	defer server.Close()
	result, err := testClient(t, server.URL, []string{"1"}, []domain.MediaType{domain.MediaMovie}).Read(context.Background(), requirements([]string{"1"}, []domain.MediaType{domain.MediaMovie}))
	if err != nil || !result.Complete || len(result.Items) != 0 || !hasCoverage(result, "1", domain.MediaMovie, inventory.CoverageVerifiedComplete) {
		t.Fatalf("result=%+v error=%v", result, err)
	}
}

func TestReadQualifiedSourceItemIDLengthBoundary(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		ratingKey string
		wantErr   error
	}{
		{name: "maximum permitted", ratingKey: strings.Repeat("r", maxSourceItemID-len("1:"))},
		{name: "one byte over", ratingKey: strings.Repeat("r", maxSourceItemID-len("1:")+1), wantErr: ErrResponse},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/library/sections" {
					_, _ = w.Write([]byte(`{"MediaContainer":{"Directory":[{"key":"1","title":"Movies","type":"movie"}]}}`))
					return
				}
				_, _ = w.Write([]byte(`{"MediaContainer":{"offset":0,"size":1,"totalSize":1,"Metadata":[{"ratingKey":"` + testCase.ratingKey + `","type":"movie","title":"Title"}]}}`))
			}))
			defer server.Close()

			result, err := testClient(t, server.URL, []string{"1"}, []domain.MediaType{domain.MediaMovie}).Read(context.Background(), requirements([]string{"1"}, []domain.MediaType{domain.MediaMovie}))
			if !errors.Is(err, testCase.wantErr) || (testCase.wantErr == nil && (!result.Complete || len(result.Items) != 1 || len(result.Items[0].SourceItemID) != maxSourceItemID)) || (testCase.wantErr != nil && (result.Complete || len(result.Items) != 0 || len(result.Coverage) != 0)) {
				t.Fatalf("result=%+v error=%v", result, err)
			}
		})
	}
}

func TestReadProducesExactCoverageForMovieShowAndArtistSections(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/library/sections" {
			_, _ = w.Write([]byte(`{"MediaContainer":{"Directory":[{"key":"movies","title":"Movies","type":"movie"},{"key":"shows","title":"Shows","type":"show"},{"key":"music","title":"Music","type":"artist"}]}}`))
			return
		}
		_, _ = w.Write([]byte(`{"MediaContainer":{"offset":0,"size":0,"totalSize":0,"Metadata":[]}}`))
	}))
	defer server.Close()

	types := []domain.MediaType{domain.MediaMovie, domain.MediaTV, domain.MediaAlbum, domain.MediaSong}
	result, err := testClient(t, server.URL, []string{"movies", "shows", "music"}, types).Read(context.Background(), requirements([]string{"movies", "shows", "music"}, types))
	if err != nil || !result.Complete || len(result.Items) != 0 || len(result.Coverage) != 12 {
		t.Fatalf("result=%+v error=%v", result, err)
	}
	for _, testCase := range []struct {
		library string
		media   domain.MediaType
		status  inventory.CoverageStatus
	}{
		{"movies", domain.MediaMovie, inventory.CoverageVerifiedComplete},
		{"shows", domain.MediaTV, inventory.CoverageVerifiedComplete},
		{"music", domain.MediaAlbum, inventory.CoverageVerifiedComplete},
		{"music", domain.MediaSong, inventory.CoverageVerifiedComplete},
		{"movies", domain.MediaTV, inventory.CoverageInapplicable},
		{"shows", domain.MediaAlbum, inventory.CoverageInapplicable},
		{"music", domain.MediaMovie, inventory.CoverageInapplicable},
	} {
		if !hasCoverage(result, testCase.library, testCase.media, testCase.status) {
			t.Fatalf("missing coverage library=%q type=%q status=%q: %+v", testCase.library, testCase.media, testCase.status, result.Coverage)
		}
	}
}

func TestReadCategorizesFailuresWithoutLeakingToken(t *testing.T) {
	for _, testCase := range []struct {
		name, body string
		status     int
		want       error
	}{
		{"authentication", "", http.StatusUnauthorized, ErrAuthentication},
		{"bad response", "", http.StatusBadGateway, ErrResponse},
		{"duplicate JSON", `{"MediaContainer":{"Directory":[],"Directory":[]}}`, http.StatusOK, ErrResponse},
		{"incomplete pagination", `{"MediaContainer":{"offset":0,"size":1,"totalSize":2,"Metadata":[{"ratingKey":"1","type":"movie","title":"One"}]}}`, http.StatusOK, ErrResponse},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(testCase.status)
				if testCase.body != "" {
					_, _ = w.Write([]byte(testCase.body))
					return
				}
				if r.URL.Path == "/library/sections" {
					_, _ = w.Write([]byte(`{"MediaContainer":{"Directory":[{"key":"1","title":"Movies","type":"movie"}]}}`))
				}
			}))
			defer server.Close()
			result, err := testClient(t, server.URL, []string{"1"}, []domain.MediaType{domain.MediaMovie}).Read(context.Background(), requirements([]string{"1"}, []domain.MediaType{domain.MediaMovie}))
			if !errors.Is(err, testCase.want) || strings.Contains(err.Error(), "synthetic-token") || result.Complete || len(result.Coverage) != 0 {
				t.Fatalf("result=%+v error=%v", result, err)
			}
		})
	}
}

func TestReadCancellationAndDestinationSafety(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := testClient(t, server.URL, []string{"1"}, []domain.MediaType{domain.MediaMovie}).Read(ctx, requirements([]string{"1"}, []domain.MediaType{domain.MediaMovie}))
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("cancellation error=%v", err)
	}

	client, err := NewClient(Options{BaseURL: "http://plex.example", SourceID: "source", TokenProvider: fixedToken("token"), Resolver: fixedResolver{netip.MustParseAddr("127.0.0.1")}})
	if !errors.Is(err, ErrInsecureHTTP) {
		t.Fatalf("default HTTP error=%v", err)
	}
	if client != nil {
		t.Fatal("default HTTP returned a client")
	}
}

func TestReadAllowsExplicitLoopbackHTTP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"MediaContainer":{"Directory":[{"key":"1","title":"Movies","type":"movie"}]}}`))
	}))
	defer server.Close()

	result, err := testClient(t, server.URL, []string{"1"}, []domain.MediaType{domain.MediaMovie}).Read(context.Background(), requirements([]string{"1"}, []domain.MediaType{domain.MediaMovie}))
	if err != nil || !result.Complete {
		t.Fatalf("result=%+v error=%v", result, err)
	}
}

func TestReadRejectsExplicitHTTPForNonLoopbackDestination(t *testing.T) {
	client, err := NewClient(Options{BaseURL: "http://plex.example", SourceID: "source", TokenProvider: fixedToken("token"), Resolver: fixedResolver{netip.MustParseAddr("192.0.2.10")}, AllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")}, AllowLoopbackHTTP: true})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Read(context.Background(), requirements([]string{"1"}, []domain.MediaType{domain.MediaMovie}))
	if !errors.Is(err, ErrDestination) {
		t.Fatalf("non-loopback HTTP error=%v", err)
	}
}

func TestResolveExplicitHTTPDestinations(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		baseURL   string
		addresses []netip.Addr
		want      []netip.Addr
		wantErr   error
		calls     int
	}{
		{name: "IPv4 loopback literal", baseURL: "http://127.0.0.1", want: []netip.Addr{netip.MustParseAddr("127.0.0.1")}},
		{name: "IPv4 non-loopback literal", baseURL: "http://192.0.2.10", wantErr: ErrDestination},
		{name: "IPv6 loopback literal", baseURL: "http://[::1]", want: []netip.Addr{netip.MustParseAddr("::1")}},
		{name: "IPv4-mapped IPv6 loopback literal", baseURL: "http://[::ffff:127.0.0.1]", want: []netip.Addr{netip.MustParseAddr("127.0.0.1")}},
		{name: "mixed DNS answers", baseURL: "http://plex.example", addresses: []netip.Addr{netip.MustParseAddr("127.0.0.1"), netip.MustParseAddr("192.0.2.10")}, wantErr: ErrDestination, calls: 1},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			resolver := &recordingResolver{addresses: testCase.addresses}
			client, err := NewClient(Options{BaseURL: testCase.baseURL, SourceID: "source", TokenProvider: fixedToken("synthetic-token"), Resolver: resolver, AllowLoopbackHTTP: true})
			if err != nil {
				t.Fatal(err)
			}

			got, err := client.resolve(context.Background())
			if !errors.Is(err, testCase.wantErr) || !slicesEqual(got, testCase.want) || resolver.calls != testCase.calls {
				t.Fatalf("addresses=%v error=%v resolver calls=%d", got, err, resolver.calls)
			}
		})
	}
}

func TestReadRejectsMixedDNSAnswersWithoutLeakingToken(t *testing.T) {
	const token = "plex-token-must-not-leak"
	client, err := NewClient(Options{BaseURL: "http://plex.example", SourceID: "source", TokenProvider: fixedToken(token), Resolver: fixedResolver{netip.MustParseAddr("127.0.0.1"), netip.MustParseAddr("192.0.2.10")}, AllowLoopbackHTTP: true})
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.Read(context.Background(), requirements([]string{"1"}, []domain.MediaType{domain.MediaMovie}))
	if !errors.Is(err, ErrDestination) || strings.Contains(err.Error(), token) {
		t.Fatalf("error=%v", err)
	}
}

func TestHTTPClientPinsDialToApprovedResolvedAddress(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	serverURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	dialer := &recordingDialer{target: serverURL.Host}
	client, err := NewClient(Options{BaseURL: "http://plex.example:" + serverURL.Port(), SourceID: "source", TokenProvider: fixedToken("synthetic-token"), Resolver: fixedResolver{netip.MustParseAddr("127.0.0.1")}, Dialer: dialer, AllowLoopbackHTTP: true})
	if err != nil {
		t.Fatal(err)
	}

	addresses, err := client.resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.httpClient(addresses).Get(client.baseURL.String())
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent || dialer.address != net.JoinHostPort("127.0.0.1", serverURL.Port()) {
		t.Fatalf("status=%d dial address=%q", response.StatusCode, dialer.address)
	}
}

func TestNewClientAllowsHTTPS(t *testing.T) {
	client, err := NewClient(Options{BaseURL: "https://plex.example", SourceID: "source", TokenProvider: fixedToken("token"), Resolver: fixedResolver{netip.MustParseAddr("192.0.2.10")}})
	if err != nil {
		t.Fatalf("HTTPS client error=%v", err)
	}
	if client == nil {
		t.Fatal("HTTPS returned no client")
	}
}

func testClient(t *testing.T, rawURL string, libraries []string, mediaTypes []domain.MediaType) *Client {
	t.Helper()
	base, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(Options{BaseURL: "http://plex.example:" + base.Port(), SourceID: "source", TokenProvider: fixedToken("synthetic-token"), Resolver: fixedResolver{netip.MustParseAddr("127.0.0.1")}, Dialer: forwardingDialer{target: base.Host}, AllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}, AllowLoopbackHTTP: true, Now: func() time.Time { return time.Unix(1, 0) }})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func requirements(libraries []string, mediaTypes []domain.MediaType) inventory.CompleteReaderRequirements {
	return inventory.CompleteReaderRequirements{SourceID: "source", SelectedLibraries: libraries, EnabledMediaTypes: mediaTypes}
}

func slicesEqual(left, right []netip.Addr) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func hasCoverage(result inventory.CompleteReaderResult, libraryID string, mediaType domain.MediaType, status inventory.CoverageStatus) bool {
	for _, coverage := range result.Coverage {
		if coverage.RemoteLibraryID == libraryID && coverage.MediaType == mediaType && coverage.Status == status {
			return true
		}
	}
	return false
}

func metadata(start, count int) string {
	values := make([]string, 0, count)
	for i := start; i < start+count; i++ {
		values = append(values, `{"ratingKey":"`+strconv.Itoa(i)+`","type":"movie","title":"Title","year":2024,"Guid":[{"id":"tmdb://`+strconv.Itoa(i)+`"}]}`)
	}
	return strings.Join(values, ",")
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}
