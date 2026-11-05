// Package notifications contains persistence-independent notification types
// and delivery contracts.
package notifications

import (
	"context"
	"time"
)

type InboxItem struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Body  string `json:"body"`
	// Link is an application path to open for this notice, when it has one.
	Link      *string    `json:"link"`
	CreatedAt time.Time  `json:"createdAt"`
	ReadAt    *time.Time `json:"readAt,omitempty"`
}

// InboxRepository is implemented by the persistence adapter; user scoping is
// explicit in each operation to prevent cross-account access.
type InboxRepository interface {
	ListInbox(context.Context, string) ([]InboxItem, error)
	MarkInboxRead(context.Context, string, string) (bool, error)
}
