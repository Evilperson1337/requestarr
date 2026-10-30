package tmdb

import (
	"context"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/providers/metadata"
)

func (r *Resolver) credits(ctx context.Context, endpoint string, headers http.Header, movie *domain.MovieDetails) {
	budget := 1500 * time.Millisecond
	if deadline, ok := ctx.Deadline(); ok {
		// Keep a small margin so optional enrichment cannot expire an already
		// resolved core movie's overall lookup context.
		remaining := time.Until(deadline) - 200*time.Millisecond
		if remaining < budget {
			budget = remaining
		}
	}
	if budget <= 0 {
		movie.DetailsUnavailable = true
		return
	}
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	var credits *struct {
		Crew []struct {
			Job  string `json:"job"`
			Name string `json:"name"`
		} `json:"crew"`
		Cast []struct {
			Name      string `json:"name"`
			Character string `json:"character"`
		} `json:"cast"`
	}
	if err := metadata.Fetch(ctx, r.client, endpoint, headers, &credits); err != nil || credits == nil {
		movie.DetailsUnavailable = true
		return
	}
	seen := map[string]bool{}
	for _, person := range credits.Crew {
		name := cleanText(person.Name, 256)
		if person.Job == "Director" && name != "" && !seen[name] && len(movie.Directors) < 10 {
			movie.Directors = append(movie.Directors, name)
			seen[name] = true
		}
	}
	for _, person := range credits.Cast {
		if len(movie.Cast) == 5 {
			break
		}
		if name := cleanText(person.Name, 256); name != "" {
			movie.Cast = append(movie.Cast, domain.MovieCast{Name: name, Character: cleanText(person.Character, 256)})
		}
	}
}

func cleanText(value string, limit int) string {
	if !utf8.ValidString(value) || len(value) > limit || strings.IndexFunc(value, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\t' }) >= 0 {
		return ""
	}
	return strings.TrimSpace(value)
}

func imageURL(path, size string) string {
	if len(path) < 2 || len(path) > 256 || path[0] != '/' || strings.Contains(path, "..") {
		return ""
	}
	for _, r := range path[1:] {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-') {
			return ""
		}
	}
	return "https://image.tmdb.org/t/p/" + size + path
}
