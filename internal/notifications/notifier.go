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
	Event Event
	Title string
	Body  string
}

type Notifier interface {
	Name() string
	Send(context.Context, Message) error
}
