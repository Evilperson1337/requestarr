package igdb_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/providers/metadata"
	"github.com/evilperson1337/requestarr/internal/providers/metadata/igdb"
)

func TestSearchEscapesHostileTextSoItCannotChangeTheQuery(t *testing.T) {
	fake := &twitchAndIGDB{games: `[]`}
	hostile := `x"; where id = 1; fields *; \" limit 500; "`
	_, err := igdb.New(clientID, clientSecret, fake.client()).Search(context.Background(), metadata.SearchQuery{Text: hostile, Page: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.queries) != 1 {
		t.Fatalf("queries = %d", len(fake.queries))
	}
	q := fake.queries[0]
	want := `search "x\"; where id = 1; fields *; \\\" limit 500; \"";`
	if !strings.Contains(q, want) {
		t.Fatalf("search string not escaped:\n%s\nwant it to contain %s", q, want)
	}
	// Only the one search statement may hold the user's text: the rest of the
	// query is ours, so after removing it nothing hostile may remain.
	rest := strings.Replace(q, want, "", 1)
	if strings.Contains(rest, "drop") || strings.Count(rest, "where") != 1 || strings.Count(rest, "limit") != 1 {
		t.Fatalf("user text leaked outside the search string: %s", rest)
	}
}

func TestSearchTurnsControlCharactersIntoSpaces(t *testing.T) {
	fake := &twitchAndIGDB{games: `[]`}
	_, err := igdb.New(clientID, clientSecret, fake.client()).Search(context.Background(), metadata.SearchQuery{Text: "a\n;where id=1\tb", Page: 1})
	if err != nil || !strings.Contains(fake.queries[0], `search "a ;where id=1 b";`) {
		t.Fatalf("%v: %s", err, fake.queries)
	}
}

func fixtureSearch(t *testing.T, page int) (domain.SearchPage, *twitchAndIGDB) {
	t.Helper()
	body, err := os.ReadFile("testdata/search_games.json")
	if err != nil {
		t.Fatal(err)
	}
	fake := &twitchAndIGDB{games: string(body)}
	got, err := igdb.New(clientID, clientSecret, fake.client()).Search(context.Background(), metadata.SearchQuery{Text: "witcher", Page: page})
	if err != nil {
		t.Fatal(err)
	}
	return got, fake
}

func TestSearchShowsGamesAndRemakesButHidesDLCUpdatesAndBundles(t *testing.T) {
	got, fake := fixtureSearch(t, 1)
	var titles []string
	for _, r := range got.Results {
		titles = append(titles, r.Title)
	}
	want := []string{"The Witcher 3: Wild Hunt", "Resident Evil 4 (Remake)", "Shadow of the Colossus Remaster", "Doom 64 Port", "Skyrim Special Edition"}
	if strings.Join(titles, "|") != strings.Join(want, "|") {
		t.Fatalf("titles = %v\nwant %v", titles, want)
	}
	// The same rule is asked of IGDB, so a page is not mostly hidden rows.
	if !strings.Contains(fake.queries[0], "where game_type = (0,8,9,10,11);") {
		t.Fatalf("kind filter missing: %s", fake.queries[0])
	}
}

func TestSearchRowsCarryYearCoverAndAShortPlatformHint(t *testing.T) {
	got, _ := fixtureSearch(t, 1)
	witcher, doom, skyrim := got.Results[0], got.Results[3], got.Results[4]
	if witcher.Year != 2015 || witcher.ProviderID != "1942" || witcher.Provider != "igdb" || witcher.MediaType != domain.MediaGame || witcher.ArtworkKind != domain.ArtworkPoster ||
		witcher.ArtworkURL != "https://images.igdb.com/igdb/image/upload/t_cover_big/co1wyy.jpg" {
		t.Fatalf("unexpected row: %+v", witcher)
	}
	if witcher.Subtitle != "PC, PS4, XONE +1" {
		t.Fatalf("hint = %q", witcher.Subtitle)
	}
	if got.Results[2].Subtitle != "PlayStation 3" {
		t.Fatalf("falls back to the name: %q", got.Results[2].Subtitle)
	}
	if doom.ArtworkURL != "" || doom.Year != 0 || skyrim.Subtitle != "" || skyrim.ArtworkURL != "" {
		t.Fatalf("missing or unsafe data must be empty: %+v %+v", doom, skyrim)
	}
}

// Picking a result runs the resolver unchanged, so the resolver has to accept
// every ref a search produced and ask IGDB for exactly that game.
func TestEveryFixtureResultRefIsAcceptedByTheResolver(t *testing.T) {
	got, _ := fixtureSearch(t, 1)
	if len(got.Results) == 0 {
		t.Fatal("no results")
	}
	for _, r := range got.Results {
		u, err := metadata.ParseURL(r.Ref)
		if err != nil {
			t.Errorf("%s: pipeline rejects ref %q: %v", r.Title, r.Ref, err)
			continue
		}
		fake := &twitchAndIGDB{games: `[{"id":1,"slug":"x","name":"X"}]`}
		resolver := igdb.New(clientID, clientSecret, fake.client())
		if !resolver.CanResolve(u) {
			t.Errorf("%s: resolver would not take ref %q", r.Title, r.Ref)
			continue
		}
		if _, err := resolver.Resolve(context.Background(), u); err != nil {
			t.Errorf("%s: Resolve(%q): %v", r.Title, r.Ref, err)
			continue
		}
		if !strings.Contains(fake.queries[0], `where slug = "`+strings.TrimPrefix(r.Ref, "https://www.igdb.com/games/")+`";`) {
			t.Errorf("%s: resolver asked for a different game: %s", r.Title, fake.queries[0])
		}
	}
}

func TestSearchPagesWithOffsetAndReportsMore(t *testing.T) {
	_, fake := fixtureSearch(t, 3)
	if !strings.Contains(fake.queries[0], "limit 21; offset 40;") {
		t.Fatalf("page 3 must start at offset 40: %s", fake.queries[0])
	}
	full := make([]string, 21)
	for i := range full {
		full[i] = fmt.Sprintf(`{"id":%d,"slug":"game-%d","name":"Game %d","game_type":0}`, i+1, i, i)
	}
	fake = &twitchAndIGDB{games: "[" + strings.Join(full, ",") + "]"}
	got, err := igdb.New(clientID, clientSecret, fake.client()).Search(context.Background(), metadata.SearchQuery{Text: "game", Page: 1})
	if err != nil || !got.HasMore || len(got.Results) != 20 {
		t.Fatalf("21 rows is a full page plus a hint of more: %d more=%v %v", len(got.Results), got.HasMore, err)
	}
}

func TestSearchFailuresMapToSharedErrors(t *testing.T) {
	for status, want := range map[int]error{429: metadata.ErrBusy, 503: metadata.ErrBusy} {
		calls := 0
		client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.Host == "id.twitch.tv" {
				return jsonResponse(200, `{"access_token":"t","expires_in":5000000,"token_type":"bearer"}`), nil
			}
			calls++
			return jsonResponse(status, `{}`), nil
		})}
		_, err := igdb.New(clientID, clientSecret, client).Search(context.Background(), metadata.SearchQuery{Text: "witcher", Page: 1})
		if !errors.Is(err, want) || calls != 1 {
			t.Errorf("status %d: err = %v after %d calls, want %v after one", status, err, calls, want)
		}
	}
	_, err := igdb.New("", "", nil).Search(context.Background(), metadata.SearchQuery{Text: "witcher", Page: 1})
	if !errors.Is(err, metadata.ErrNotConfigured) {
		t.Errorf("unconfigured: %v", err)
	}
}

func TestSearchAnswersBusyWhenTooManyAreInFlight(t *testing.T) {
	release := make(chan struct{})
	var mu sync.Mutex
	inFlight, peak := 0, 0
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "id.twitch.tv" {
			return jsonResponse(200, `{"access_token":"t","expires_in":5000000,"token_type":"bearer"}`), nil
		}
		mu.Lock()
		inFlight++
		peak = max(peak, inFlight)
		mu.Unlock()
		<-release
		mu.Lock()
		inFlight--
		mu.Unlock()
		return jsonResponse(200, `[]`), nil
	})}
	provider := igdb.New(clientID, clientSecret, client)
	provider.SetSearchWait(20 * time.Millisecond)
	const callers = 8
	errs := make(chan error, callers)
	for i := 0; i < callers; i++ {
		go func() {
			_, err := provider.Search(context.Background(), metadata.SearchQuery{Text: "witcher", Page: 1})
			errs <- err
		}()
	}
	busy := 0
	for i := 0; i < callers-igdb.MaxSearchesInFlight; i++ {
		if err := <-errs; errors.Is(err, metadata.ErrBusy) {
			busy++
		}
	}
	close(release)
	for i := 0; i < igdb.MaxSearchesInFlight; i++ {
		<-errs
	}
	mu.Lock()
	defer mu.Unlock()
	if busy != callers-igdb.MaxSearchesInFlight || peak > igdb.MaxSearchesInFlight {
		t.Fatalf("busy answers = %d (want %d), peak concurrency %d (cap %d)", busy, callers-igdb.MaxSearchesInFlight, peak, igdb.MaxSearchesInFlight)
	}
}
