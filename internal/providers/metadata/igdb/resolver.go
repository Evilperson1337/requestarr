package igdb

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/providers/metadata"
)

const (
	tokenEndpoint = "https://id.twitch.tv/oauth2/token"
	gamesEndpoint = "https://api.igdb.com/v4/games"
	// tokenMargin renews a token this long before it expires.
	tokenMargin = time.Minute
)

// slugPattern is what an IGDB page URL names a game by. Anything else could
// carry query text into the Apifast request body, so it is refused.
var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,127}$`)

// Resolver resolves igdb.com game URLs. IGDB authenticates with a Twitch app
// access token, which it mints from the client ID and client secret and
// reuses until shortly before it expires.
type Resolver struct {
	client               *http.Client
	clientID, secret     string
	mu                   sync.Mutex
	accessToken          string
	accessTokenExpiresAt time.Time
	now                  func() time.Time
}

func New(clientID, clientSecret string, client *http.Client) *Resolver {
	if client == nil {
		client = &http.Client{Timeout: 9 * time.Second}
	}
	return &Resolver{client: client, clientID: clientID, secret: clientSecret, now: time.Now}
}

func (*Resolver) Name() string { return "igdb" }

func (*Resolver) CanResolve(u *url.URL) bool {
	if u == nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return (host == "igdb.com" || host == "www.igdb.com") && (u.Port() == "" || u.Port() == "443")
}

// token returns a usable access token, minting one when none is cached.
func (r *Resolver) token(ctx context.Context, renew bool) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !renew && r.accessToken != "" && r.now().Before(r.accessTokenExpiresAt.Add(-tokenMargin)) {
		return r.accessToken, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	form := url.Values{"client_id": {r.clientID}, "client_secret": {r.secret}, "grant_type": {"client_credentials"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "Requestarr/1.0 (+https://github.com/evilperson1337/requestarr)")
	resp, err := r.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("IGDB token request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return "", tokenError(resp.StatusCode, resp.Status)
	}
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
		TokenType   string `json:"token_type"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&out); err != nil || out.AccessToken == "" || !strings.EqualFold(out.TokenType, "bearer") || out.ExpiresIn <= 0 {
		return "", fmt.Errorf("IGDB token response was not a bearer token")
	}
	r.accessToken = out.AccessToken
	r.accessTokenExpiresAt = r.now().Add(time.Duration(out.ExpiresIn) * time.Second)
	return r.accessToken, nil
}

type gameRow struct {
	ID           uint64  `json:"id"`
	Slug         string  `json:"slug"`
	Name         string  `json:"name"`
	Summary      string  `json:"summary"`
	FirstRelease int64   `json:"first_release_date"`
	TotalRating  float64 `json:"total_rating"`
	RatingCount  int64   `json:"total_rating_count"`
	Cover        struct {
		ImageID string `json:"image_id"`
	} `json:"cover"`
	Screenshots []struct {
		ImageID string `json:"image_id"`
	} `json:"screenshots"`
	Genres []struct {
		Name string `json:"name"`
	} `json:"genres"`
	Companies []struct {
		Developer bool `json:"developer"`
		Company   struct {
			Name string `json:"name"`
		} `json:"company"`
	} `json:"involved_companies"`
	Platforms []struct {
		ID   uint64 `json:"id"`
		Name string `json:"name"`
	} `json:"platforms"`
}

const fields = "fields slug,name,summary,first_release_date,total_rating,total_rating_count,cover.image_id,screenshots.image_id,genres.name,involved_companies.developer,involved_companies.company.name,platforms.id,platforms.name;"

func (r *Resolver) Resolve(ctx context.Context, u *url.URL) (domain.ResolvedItem, error) {
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 || parts[0] != "games" {
		return domain.ResolvedItem{}, fmt.Errorf("unsupported IGDB URL")
	}
	if r.clientID == "" || r.secret == "" {
		return domain.ResolvedItem{}, fmt.Errorf("IGDB is not configured: set the IGDB client ID and client secret: %w", metadata.ErrNotConfigured)
	}
	// IGDB page URLs name a game by slug; a bare numeric ID is accepted too.
	var where string
	if id, err := strconv.ParseUint(parts[1], 10, 64); err == nil && len(parts[1]) <= 20 {
		where = fmt.Sprintf("where id = %d;", id)
	} else if slugPattern.MatchString(parts[1]) {
		where = `where slug = "` + parts[1] + `";`
	} else {
		return domain.ResolvedItem{}, fmt.Errorf("invalid IGDB game identifier")
	}
	rows, err := r.fetch(ctx, fields+" "+where+" limit 1;")
	if err != nil {
		return domain.ResolvedItem{}, err
	}
	if len(rows) != 1 || rows[0].Name == "" || rows[0].ID == 0 {
		return domain.ResolvedItem{}, fmt.Errorf("IGDB game not found")
	}
	return toResolved(rows[0]), nil
}

// fetch posts the query, renewing the token once if IGDB rejects it.
func (r *Resolver) fetch(ctx context.Context, query string) ([]gameRow, error) {
	for attempt := 0; ; attempt++ {
		token, err := r.token(ctx, attempt > 0)
		if err != nil {
			return nil, err
		}
		h := http.Header{"Client-ID": {r.clientID}, "Authorization": {"Bearer " + token}, "Content-Type": {"text/plain"}}
		var rows []gameRow
		err = metadata.FetchWithBodyRetrying(ctx, r.client, gamesEndpoint, query, h, &rows)
		if err != nil && attempt == 0 && strings.Contains(err.Error(), "401") {
			continue
		}
		return rows, err
	}
}

func toResolved(g gameRow) domain.ResolvedItem {
	id := strconv.FormatUint(g.ID, 10)
	ref := id
	if g.Slug != "" && slugPattern.MatchString(g.Slug) {
		ref = g.Slug
	}
	item := domain.CatalogItem{MediaType: domain.MediaGame, Provider: "igdb", ProviderID: id, Title: g.Name, ArtworkKind: domain.ArtworkPoster,
		ExternalURL: "https://www.igdb.com/games/" + url.PathEscape(ref), Metadata: map[string]any{"summary": g.Summary}}
	details := &domain.GameDetails{Overview: clean(g.Summary, 10000), RatingCount: 0}
	if g.FirstRelease > 0 {
		released := time.Unix(g.FirstRelease, 0).UTC()
		item.Year = released.Year()
		details.ReleaseDate = released.Format("2006-01-02")
	}
	if image := validImageID(g.Cover.ImageID); image != "" {
		item.ArtworkURL = "https://images.igdb.com/igdb/image/upload/t_cover_big/" + image + ".jpg"
	}
	if len(g.Screenshots) > 0 {
		if image := validImageID(g.Screenshots[0].ImageID); image != "" {
			details.BackdropURL = "https://images.igdb.com/igdb/image/upload/t_screenshot_big/" + image + ".jpg"
		}
	}
	for _, genre := range g.Genres {
		if name := clean(genre.Name, 80); name != "" && len(details.Genres) < 20 {
			details.Genres = append(details.Genres, name)
		}
	}
	for _, company := range g.Companies {
		if name := clean(company.Company.Name, 256); company.Developer && name != "" && len(details.Developers) < 10 {
			details.Developers = append(details.Developers, name)
		}
	}
	if g.RatingCount > 0 && g.RatingCount <= 1000000000 && g.TotalRating >= 0 && g.TotalRating <= 100 {
		score := g.TotalRating
		details.Score, details.RatingCount = &score, g.RatingCount
	}
	out := domain.ResolvedItem{Item: item, Game: details}
	for _, p := range g.Platforms {
		if p.ID > 0 && p.Name != "" {
			out.Platforms = append(out.Platforms, domain.Platform{ProviderID: strconv.FormatUint(p.ID, 10), Key: "igdb:" + strconv.FormatUint(p.ID, 10), Name: p.Name})
		}
	}
	return out
}

// validImageID admits only the characters IGDB image ids use.
func validImageID(id string) string {
	if id == "" || len(id) >= 128 {
		return ""
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9') {
			return ""
		}
	}
	return id
}

func clean(s string, limit int) string {
	s = strings.TrimSpace(strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s))
	if len(s) > limit {
		s = s[:limit]
	}
	return s
}

// tokenError tags a failed token request: bad credentials are the
// administrator's to fix, a busy Twitch is not.
func tokenError(code int, status string) error {
	switch {
	case code == http.StatusBadRequest || code == http.StatusUnauthorized || code == http.StatusForbidden:
		return fmt.Errorf("IGDB token request returned %s; check the IGDB client ID and client secret: %w", status, metadata.ErrCredentials)
	case code == http.StatusTooManyRequests || code >= 500:
		return fmt.Errorf("IGDB token request returned %s: %w", status, metadata.ErrBusy)
	}
	return fmt.Errorf("IGDB token request returned %s", status)
}
