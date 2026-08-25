package domain

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	maxAudiobookPeople       = 32
	maxAudiobookAlternateIDs = 16
	maxAudiobookRuntime      = 7 * 24 * 60 * 60
)

var (
	audibleASINPattern          = regexp.MustCompile(`^[A-Z0-9]{10}$`)
	alternateIDNamespacePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)
)

// AlternateStableID is a provider-neutral, bounded stable identifier for an
// audiobook edition. It deliberately excludes provider payloads and URLs.
type AlternateStableID struct {
	Namespace string `json:"namespace"`
	Value     string `json:"value"`
}

// AudiobookDetails contains normalized, non-secret evidence for one audiobook
// edition or manifestation. It intentionally does not model a written work or
// a work-to-manifestation hierarchy; ebook V2 may introduce that relationship.
type AudiobookDetails struct {
	ASIN           string              `json:"asin,omitempty"`
	AudibleRegion  string              `json:"audibleRegion,omitempty"`
	Authors        []string            `json:"authors,omitempty"`
	Narrators      []string            `json:"narrators,omitempty"`
	Series         string              `json:"series,omitempty"`
	SeriesPosition string              `json:"seriesPosition,omitempty"`
	RuntimeSeconds int                 `json:"runtimeSeconds,omitempty"`
	Language       string              `json:"language,omitempty"`
	Publisher      string              `json:"publisher,omitempty"`
	ReleaseDate    string              `json:"releaseDate,omitempty"`
	EditionType    string              `json:"editionType,omitempty"`
	Abridged       *bool               `json:"abridged,omitempty"`
	AlternateIDs   []AlternateStableID `json:"alternateIds,omitempty"`
}

// NormalizeAudiobookDetails validates details and returns its canonical form.
// Text is trimmed, authors, narrators, and alternate IDs retain first-seen
// order while removing duplicates, and ASIN and region use canonical casing.
func NormalizeAudiobookDetails(details AudiobookDetails) (AudiobookDetails, error) {
	details.ASIN = strings.ToUpper(strings.TrimSpace(details.ASIN))
	details.AudibleRegion = strings.ToUpper(strings.TrimSpace(details.AudibleRegion))
	if (details.ASIN == "") != (details.AudibleRegion == "") {
		return AudiobookDetails{}, fmt.Errorf("ASIN and Audible region must be provided together")
	}
	if details.ASIN != "" && !audibleASINPattern.MatchString(details.ASIN) {
		return AudiobookDetails{}, fmt.Errorf("ASIN must be 10 uppercase alphanumeric characters")
	}
	if details.AudibleRegion != "" && !isAudibleRegion(details.AudibleRegion) {
		return AudiobookDetails{}, fmt.Errorf("Audible region must be a two-letter region code")
	}

	var err error
	if details.Authors, err = normalizeTextList(details.Authors, maxAudiobookPeople, 128, "author"); err != nil {
		return AudiobookDetails{}, err
	}
	if details.Narrators, err = normalizeTextList(details.Narrators, maxAudiobookPeople, 128, "narrator"); err != nil {
		return AudiobookDetails{}, err
	}
	for _, field := range []struct {
		name  string
		value *string
		max   int
	}{
		{"series", &details.Series, 256},
		{"series position", &details.SeriesPosition, 64},
		{"language", &details.Language, 35},
		{"publisher", &details.Publisher, 256},
		{"edition type", &details.EditionType, 64},
	} {
		if *field.value, err = normalizeText(*field.value, field.max, field.name); err != nil {
			return AudiobookDetails{}, err
		}
	}
	if details.RuntimeSeconds < 0 || details.RuntimeSeconds > maxAudiobookRuntime {
		return AudiobookDetails{}, fmt.Errorf("runtime seconds must be between 0 and %d", maxAudiobookRuntime)
	}
	if details.ReleaseDate, err = normalizeReleaseDate(details.ReleaseDate); err != nil {
		return AudiobookDetails{}, err
	}
	if details.AlternateIDs, err = normalizeAlternateIDs(details.AlternateIDs); err != nil {
		return AudiobookDetails{}, err
	}
	return details, nil
}

func isAudibleRegion(region string) bool {
	return len(region) == 2 && region[0] >= 'A' && region[0] <= 'Z' && region[1] >= 'A' && region[1] <= 'Z'
}

func normalizeTextList(values []string, maxItems, maxBytes int, name string) ([]string, error) {
	if len(values) > maxItems {
		return nil, fmt.Errorf("too many %ss", name)
	}
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		normalized, err := normalizeText(value, maxBytes, name)
		if err != nil {
			return nil, err
		}
		if normalized == "" {
			return nil, fmt.Errorf("%s must not be empty", name)
		}
		key := strings.ToLower(normalized)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, normalized)
	}
	return result, nil
}

func normalizeText(value string, maxBytes int, field string) (string, error) {
	if !utf8.ValidString(value) {
		return "", fmt.Errorf("%s must be valid UTF-8", field)
	}
	value = strings.TrimSpace(value)
	if len(value) > maxBytes {
		return "", fmt.Errorf("%s exceeds %d bytes", field, maxBytes)
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return "", fmt.Errorf("%s contains control characters", field)
		}
	}
	return value, nil
}

func normalizeReleaseDate(value string) (string, error) {
	value, err := normalizeText(value, len("2006-01-02"), "release date")
	if err != nil || value == "" {
		return value, err
	}
	if _, err := time.Parse("2006-01-02", value); err != nil {
		return "", fmt.Errorf("release date must use YYYY-MM-DD: %w", err)
	}
	return value, nil
}

func normalizeAlternateIDs(ids []AlternateStableID) ([]AlternateStableID, error) {
	if len(ids) > maxAudiobookAlternateIDs {
		return nil, fmt.Errorf("too many alternate IDs")
	}
	result := make([]AlternateStableID, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id.Namespace = strings.ToLower(strings.TrimSpace(id.Namespace))
		if !alternateIDNamespacePattern.MatchString(id.Namespace) {
			return nil, fmt.Errorf("alternate ID namespace is invalid")
		}
		var err error
		if id.Value, err = normalizeText(id.Value, 128, "alternate ID value"); err != nil {
			return nil, err
		}
		if id.Value == "" {
			return nil, fmt.Errorf("alternate ID value must not be empty")
		}
		key := id.Namespace + "\x00" + id.Value
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, id)
	}
	return result, nil
}
