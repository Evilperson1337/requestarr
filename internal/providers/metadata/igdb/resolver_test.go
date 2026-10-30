package igdb_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/providers/metadata"
	"github.com/evilperson1337/requestarr/internal/providers/metadata/igdb"
)

const (
	clientID     = "test-client-id"
	clientSecret = "test-client-secret"
	mintedToken  = "minted-app-token"
)

// twitchAndIGDB stands in for id.twitch.tv and api.igdb.com. It records what
// the resolver sent and answers the way the real services do: the games
// endpoint only accepts the app access token the token endpoint minted.
type twitchAndIGDB struct {
	mu      sync.Mutex
	mints   int
	queries []string
	auths   []string
	games   string // JSON body for /v4/games
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func (f *twitchAndIGDB) client() *http.Client {
	return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		switch r.URL.Host + r.URL.Path {
		case "id.twitch.tv/oauth2/token":
			body, _ := io.ReadAll(r.Body)
			values := string(body) + "&" + r.URL.RawQuery
			if !strings.Contains(values, "client_id="+clientID) || !strings.Contains(values, "client_secret="+clientSecret) || !strings.Contains(values, "grant_type=client_credentials") {
				return jsonResponse(400, `{"status":400,"message":"invalid client"}`), nil
			}
			f.mints++
			return jsonResponse(200, `{"access_token":"`+mintedToken+`","expires_in":5000000,"token_type":"bearer"}`), nil
		case "api.igdb.com/v4/games":
			body, _ := io.ReadAll(r.Body)
			f.queries = append(f.queries, string(body))
			f.auths = append(f.auths, r.Header.Get("Authorization")+"|"+r.Header.Get("Client-ID"))
			if r.Header.Get("Authorization") != "Bearer "+mintedToken {
				return jsonResponse(401, `{"message":"Authorization Failure. Have you tried:"}`), nil
			}
			return jsonResponse(200, f.games), nil
		}
		return jsonResponse(404, `{}`), nil
	})}
}

const modernWarfare = `[{"id":1111,"slug":"call-of-duty-modern-warfare-4","name":"Call of Duty: Modern Warfare 4","summary":"The next chapter.","first_release_date":1793000000,
"cover":{"image_id":"co1abc"},"platforms":[{"id":6,"name":"PC (Microsoft Windows)"},{"id":167,"name":"PlayStation 5"}]}]`

// The exact URL from the bug report: an IGDB page URL names the game by slug.
func TestResolveAcceptsAnIGDBSlugURLAndAuthenticatesWithAMintedToken(t *testing.T) {
	fake := &twitchAndIGDB{games: modernWarfare}
	service := metadata.NewService(nil, igdb.New(clientID, clientSecret, fake.client()))
	item, err := service.Resolve(context.Background(), "https://www.igdb.com/games/call-of-duty-modern-warfare-4")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if item.Item.MediaType != domain.MediaGame || item.Item.Provider != "igdb" || item.Item.ProviderID != "1111" || item.Item.Title != "Call of Duty: Modern Warfare 4" {
		t.Fatalf("item = %+v", item.Item)
	}
	if len(item.Platforms) != 2 || item.Platforms[0].Key != "igdb:6" {
		t.Fatalf("platforms = %+v", item.Platforms)
	}
	if len(fake.queries) != 1 || !strings.Contains(fake.queries[0], `slug = "call-of-duty-modern-warfare-4"`) {
		t.Fatalf("the games query must look the slug up, got %q", fake.queries)
	}
	if fake.auths[0] != "Bearer "+mintedToken+"|"+clientID {
		t.Fatalf("games request auth = %q; the client secret is not an access token", fake.auths[0])
	}
}

func TestResolveStillAcceptsANumericIDAndReusesTheToken(t *testing.T) {
	fake := &twitchAndIGDB{games: modernWarfare}
	r := igdb.New(clientID, clientSecret, fake.client())
	service := metadata.NewService(nil, r)
	for _, raw := range []string{"https://www.igdb.com/games/1111", "https://www.igdb.com/games/call-of-duty-modern-warfare-4/"} {
		if _, err := service.Resolve(context.Background(), raw); err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
	}
	if fake.mints != 1 {
		t.Fatalf("token minted %d times; it should be cached", fake.mints)
	}
	if !strings.Contains(fake.queries[0], "where id = 1111") {
		t.Fatalf("numeric ID query = %q", fake.queries[0])
	}
}

func TestResolveRejectsSlugsThatCouldCarryQueryText(t *testing.T) {
	fake := &twitchAndIGDB{games: modernWarfare}
	service := metadata.NewService(nil, igdb.New(clientID, clientSecret, fake.client()))
	for _, slug := range []string{`x";%20fields%20*;`, "UPPER_case", strings.Repeat("a", 200)} {
		if _, err := service.Resolve(context.Background(), "https://www.igdb.com/games/"+slug); err == nil {
			t.Errorf("slug %q was accepted", slug)
		}
	}
	if len(fake.queries) != 0 {
		t.Fatalf("a rejected slug still reached IGDB: %q", fake.queries)
	}
}
