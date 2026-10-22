package domain

import "time"

type RequestStatus string

const (
	RequestRequested RequestStatus = "requested"
	RequestApproved  RequestStatus = "approved"
	RequestDenied    RequestStatus = "denied"
	RequestAvailable RequestStatus = "available"
	RequestCancelled RequestStatus = "cancelled"
)

// IsOpen reports whether a Request is still awaiting availability: only open
// Requests become available or are cancelled when their last subscriber leaves.
func (s RequestStatus) IsOpen() bool { return s == RequestRequested || s == RequestApproved }

// CanTransition encodes the admin lifecycle (D016): approve/deny decide a
// requested Request; an open Request can be cancelled. Availability is
// projected from inventory (D014), never set through this path.
func CanTransition(from, to RequestStatus) bool {
	switch to {
	case RequestApproved, RequestDenied:
		return from == RequestRequested
	case RequestCancelled:
		return from.IsOpen()
	}
	return false
}

type PlatformSelection struct {
	Any          bool     `json:"any"`
	PlatformKeys []string `json:"platformKeys,omitempty"`
}

type Request struct {
	ID                     string
	Version                int64
	CatalogItemID          string
	Title                  string
	MediaType              MediaType
	ArtworkURL             string
	ArtworkKind            ArtworkKind
	Status                 RequestStatus
	RequestedBy            string
	Note                   string
	Platforms              PlatformSelection
	PlatformNames          []string
	CreatedAt              time.Time
	UpdatedAt              time.Time
	AvailableAt            *time.Time
	Availability           Availability       `json:"availability"`
	AvailabilityObservedAt *time.Time         `json:"availabilityObservedAt"`
	SubscriptionSource     SubscriptionSource `json:"subscriptionSource"`
	Subscribed             bool               `json:"subscribed"`
}

type SubscriptionSource string

const (
	SubscriptionRequester SubscriptionSource = "requester"
	SubscriptionManual    SubscriptionSource = "manual"
)

type RequestSubscription struct {
	RequestID string
	UserID    string
	Source    SubscriptionSource
	CreatedAt time.Time
}

// ValidatePlatformSelection encodes the V1 UX rule: Any is mutually exclusive
// with explicit selections, and an explicit selection requires at least one platform.
func ValidatePlatformSelection(s PlatformSelection) bool {
	if s.Any {
		return len(s.PlatformKeys) == 0
	}
	if len(s.PlatformKeys) == 0 {
		return false
	}
	seen := make(map[string]struct{}, len(s.PlatformKeys))
	for _, key := range s.PlatformKeys {
		if key == "" {
			return false
		}
		if _, ok := seen[key]; ok {
			return false
		}
		seen[key] = struct{}{}
	}
	return true
}
