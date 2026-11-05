// Package checklist computes the administrator's "Get started" list from live
// state (D038). It holds no inputs of its own: every item is derived from what
// the instance is already configured with, and the only thing it remembers is
// that an administrator dismissed it.
package checklist

import (
	"context"
	"fmt"

	"github.com/evilperson1337/requestarr/internal/opstatus"
)

type Key string

const (
	Libraries     Key = "libraries"
	Metadata      Key = "metadata"
	Notifications Key = "notifications"
	People        Key = "people"
)

type Item struct {
	Key      Key    `json:"key"`
	Title    string `json:"title"`
	Required bool   `json:"required"`
	Done     bool   `json:"done"`
	// Detail says what is missing or what to do next; empty when done.
	Detail string `json:"detail,omitempty"`
}

type Result struct {
	Items []Item `json:"items"`
	// Dismissed is an administrator's choice, remembered for the instance.
	Dismissed bool `json:"dismissed"`
	// Visible is true while the checklist should be offered: not dismissed and
	// at least one required item is still open.
	Visible bool `json:"visible"`
}

// Status is the operational overview the items are computed from.
type Status interface {
	Overview(context.Context) (opstatus.Overview, error)
}

// Store is the small amount of stored state the checklist reads.
type Store interface {
	// PolicyReviewed reports whether an administrator ever saved the default
	// role for new people or the approval policy (D035, D036).
	PolicyReviewed(context.Context) (bool, error)
	ChecklistDismissed(context.Context) (bool, error)
	DismissChecklist(context.Context) error
}

type Service struct {
	status Status
	store  Store
}

func New(status Status, store Store) *Service { return &Service{status: status, store: store} }

func (s *Service) Get(ctx context.Context) (Result, error) {
	overview, err := s.status.Overview(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("load status: %w", err)
	}
	reviewed, err := s.store.PolicyReviewed(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("load people settings: %w", err)
	}
	dismissed, err := s.store.ChecklistDismissed(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("load checklist state: %w", err)
	}
	items := []Item{
		libraries(overview),
		metadata(overview),
		{Key: Notifications, Title: "Set up notifications", Done: providerOn(overview, "apprise"),
			Detail: doneDetail(providerOn(overview, "apprise"), "Optional. Without Apprise, people only see availability inside Requestarr.")},
		{Key: People, Title: "Review who can do what", Done: reviewed,
			Detail: doneDetail(reviewed, "Optional. New people start as viewers and every request waits for approval until you change the default role or the approval policy.")},
	}
	result := Result{Items: items, Dismissed: dismissed}
	requiredOpen := false
	for _, item := range items {
		if item.Required && !item.Done {
			requiredOpen = true
		}
	}
	result.Visible = !dismissed && requiredOpen
	return result, nil
}

func (s *Service) Dismiss(ctx context.Context) error { return s.store.DismissChecklist(ctx) }

func doneDetail(done bool, todo string) string {
	if done {
		return ""
	}
	return todo
}

func providerOn(o opstatus.Overview, key string) bool {
	for _, p := range o.Providers {
		if p.Key == key && p.Configured {
			return true
		}
	}
	return false
}

func libraries(o opstatus.Overview) Item {
	item := Item{Key: Libraries, Title: "Connect a media library", Required: true, Detail: "Connect Jellyfin, Plex, Navidrome, RomM or Audiobookshelf so Requestarr can tell what you already have."}
	for _, source := range o.Sources {
		if !source.Configured {
			continue
		}
		if source.LastSnapshotAt != nil {
			return Item{Key: Libraries, Title: item.Title, Required: true, Done: true}
		}
		item.Detail = source.Name + " is connected and waiting for its first completed sync."
		if source.Name == "" {
			item.Detail = "A library is connected and waiting for its first completed sync."
		}
	}
	return item
}

// metadata ignores MusicBrainz: it needs no setup, so it says nothing about
// whether the instance can search movies, TV or games.
func metadata(o opstatus.Overview) Item {
	item := Item{Key: Metadata, Title: "Add a metadata provider", Required: true}
	if providerOn(o, "tmdb") || providerOn(o, "igdb") {
		item.Done = true
		return item
	}
	item.Detail = "A TMDB read token turns on Movies and TV search; IGDB turns on Games. Music works without setup."
	return item
}
