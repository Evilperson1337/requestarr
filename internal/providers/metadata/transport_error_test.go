package metadata_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/providers/metadata"
	"github.com/evilperson1337/requestarr/internal/providers/metadata/audible"
	"github.com/evilperson1337/requestarr/internal/providers/metadata/musicbrainz"
	"github.com/evilperson1337/requestarr/internal/providers/metadata/tmdb"
)

type failingTransport struct{}

func (failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("connection refused")
}

// A transport failure must not carry the request URL, which holds the typed
// search text, in its error text: callers log these errors.
func TestSearchTransportErrorsNeverCarrySearchText(t *testing.T) {
	const secret = "zzdistinctivequeryzz"
	client := &http.Client{Transport: failingTransport{}}
	mb := musicbrainz.New(client)
	searchers := map[string]metadata.Searcher{
		"tmdb":    tmdb.New("token", client),
		"album":   mb,
		"song":    mb.SongSearcher(),
		"audible": audible.New(domain.AudibleRegion("us"), client),
	}
	for name, s := range searchers {
		t.Run(name, func(t *testing.T) {
			var logs bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&logs, nil))
			_, err := s.Search(context.Background(), metadata.SearchQuery{Text: secret, Page: 1})
			if err == nil {
				t.Fatal("a failing transport must be an error")
			}
			logger.Warn("metadata search failed", "error", err)
			if strings.Contains(err.Error(), secret) || strings.Contains(logs.String(), secret) {
				t.Fatalf("search text leaked: %v / %s", err, logs.String())
			}
			if !strings.Contains(err.Error(), "connection refused") {
				t.Fatalf("the error class was lost: %v", err)
			}
		})
	}
}
