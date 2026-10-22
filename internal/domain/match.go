package domain

import (
	"errors"
	"time"
)

var (
	ErrMatchNotFound        = errors.New("match suggestion not found")
	ErrMatchAlreadyReviewed = errors.New("match suggestion already reviewed")
	ErrMatchStale           = errors.New("match suggestion no longer present or matching")
	ErrMatchDecision        = errors.New("match decision must be accept or dismiss")
)

// MatchReviewTransition applies the terminal review policy without persistence
// or transport concerns. Repeating the same decision is a successful no-op;
// only a current candidate can be accepted for the first time.
func MatchReviewTransition(status, decision string, currentCandidate bool) (string, error) {
	next := "accepted"
	if decision == "dismiss" {
		next = "dismissed"
	} else if decision != "accept" {
		return status, ErrMatchDecision
	}
	if status == next {
		return status, nil
	}
	if status != "pending" {
		return status, ErrMatchAlreadyReviewed
	}
	if decision == "accept" && !currentCandidate {
		return status, ErrMatchStale
	}
	return next, nil
}

// MatchSuggestion is an unverified title/year candidate, not availability
// evidence. Only an explicit administrator review can confirm the match.
type MatchSuggestion struct {
	ID             string    `json:"id"`
	CatalogItemID  string    `json:"catalogItemId"`
	CatalogTitle   string    `json:"catalogTitle"`
	CatalogYear    int       `json:"catalogYear"`
	InventoryTitle string    `json:"inventoryTitle"`
	InventoryYear  int       `json:"inventoryYear"`
	MediaType      MediaType `json:"mediaType"`
	PlatformKey    string    `json:"platformKey"`
	SourceName     string    `json:"sourceName"`
	SourceItemID   string    `json:"sourceItemId"`
	Confidence     string    `json:"confidence"`
	Reason         string    `json:"reason"`
	ObservedAt     time.Time `json:"observedAt"`
}
