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

type PlatformSelection struct {
	Any          bool     `json:"any"`
	PlatformKeys []string `json:"platformKeys,omitempty"`
}

type Request struct {
	ID            string
	CatalogItemID string
	Status        RequestStatus
	RequestedBy   string
	Note          string
	Platforms     PlatformSelection
	CreatedAt     time.Time
	UpdatedAt     time.Time
	AvailableAt   *time.Time
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
	return len(s.PlatformKeys) > 0
}
