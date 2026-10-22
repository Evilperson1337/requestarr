package reconcile

// Package reconcile will compare normalized inventory changes against active
// requests. It should resolve only requests affected by the changed canonical IDs
// or fallback match keys. A game request is satisfied when ANY selected acceptable
// platform is present; `Any` accepts any platform for the canonical game.

import (
	"strings"

	"github.com/evilperson1337/requestarr/internal/domain"
	"github.com/evilperson1337/requestarr/internal/inventory"
)

// MatchResult separates exact confirmations from title/year review suggestions.
type MatchResult struct {
	Confirmed map[string]inventory.Item // catalog item ID -> source item
	Suggested []inventory.Item
}

// MatchSnapshot matches provider identifiers only for automatic confirmation.
// Incomplete snapshots can add positive evidence, but callers must retain any
// prior evidence because absence is not meaningful.
func MatchSnapshot(catalog []domain.CatalogItem, snapshot inventory.Snapshot) MatchResult {
	result := MatchResult{Confirmed: make(map[string]inventory.Item)}
	for _, item := range snapshot.Items {
		matched := false
		for _, candidate := range catalog {
			if exactExternalID(candidate, item.ExternalIDs) {
				result.Confirmed[candidate.ID] = item
				matched = true
				break
			}
		}
		if !matched {
			for _, candidate := range catalog {
				if candidate.MediaType == item.MediaType && strings.EqualFold(strings.TrimSpace(candidate.Title), strings.TrimSpace(item.Title)) && candidate.Year != 0 && candidate.Year == item.Year {
					result.Suggested = append(result.Suggested, item)
					break
				}
			}
		}
	}
	return result
}

func exactExternalID(candidate domain.CatalogItem, ids map[string]string) bool {
	if candidate.Provider != "tmdb" || strings.TrimSpace(candidate.ProviderID) == "" {
		return false
	}
	if candidate.MediaType != domain.MediaMovie && candidate.MediaType != domain.MediaTV {
		return false
	}
	return strings.TrimSpace(ids["tmdb"]) == strings.TrimSpace(candidate.ProviderID)
}
