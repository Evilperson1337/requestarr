package notifications

import "context"

type Event string

const (
	EventRequestCreated   Event = "request.created"
	EventRequestApproved  Event = "request.approved"
	EventRequestDenied    Event = "request.denied"
	EventRequestCancelled Event = "request.cancelled"
	EventRequestAvailable Event = "request.available"
)

type Message struct {
	Event  Event
	UserID string
	Title  string
	Body   string
	URL    string
	Tags   []string
}

type Notifier interface {
	Name() string
	Send(context.Context, Message) error
}
