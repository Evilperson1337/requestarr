package requests

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/evilperson1337/requestarr/internal/domain"
)

var (
	ErrInvalidQuery  = errors.New("invalid request-list query")
	ErrInvalidCursor = errors.New("invalid request-list cursor")
	ErrCursorExpired = errors.New("request-list cursor expired")
	ErrQueryTooLarge = errors.New("request-list query exceeds snapshot capacity")
)

// ListQuery describes a shared queue view. Mine is subscription-based. Cursor
// is opaque to the use case and excluded from the normalized query identity.
type ListQuery struct {
	Q         string
	RequestID string `json:"requestId,omitempty"`
	Status    domain.RequestStatus
	MediaType domain.MediaType
	Mine      bool
	Sort      string
	Limit     int
	Cursor    string `json:"-"`
}

type Page struct {
	Items      []domain.Request `json:"items"`
	NextCursor *string          `json:"nextCursor"`
}

// Normalize validates transport-independent query values and supplies defaults.
func (q ListQuery) Normalize() (ListQuery, error) {
	if q.RequestID != "" && !domain.ValidUUID(q.RequestID) {
		return ListQuery{}, ErrInvalidQuery
	}
	q.RequestID = strings.ToLower(q.RequestID)
	if !utf8.ValidString(q.Q) || utf8.RuneCountInString(q.Q) > 200 || strings.IndexFunc(q.Q, unicode.IsControl) >= 0 {
		return ListQuery{}, ErrInvalidQuery
	}
	q.Q = strings.ToLower(strings.TrimSpace(q.Q))
	switch q.Status {
	case "", domain.RequestRequested, domain.RequestApproved, domain.RequestAvailable, domain.RequestDenied, domain.RequestCancelled:
	default:
		return ListQuery{}, ErrInvalidQuery
	}
	switch q.MediaType {
	case "", domain.MediaMovie, domain.MediaTV, domain.MediaAlbum, domain.MediaSong, domain.MediaGame:
	default:
		return ListQuery{}, ErrInvalidQuery
	}
	if q.Sort == "" {
		q.Sort = "newest"
	}
	if q.Sort != "newest" && q.Sort != "oldest" && q.Sort != "status" {
		return ListQuery{}, ErrInvalidQuery
	}
	if q.Limit == 0 {
		q.Limit = 100
	}
	if q.Limit < 1 || q.Limit > 100 {
		return ListQuery{}, ErrInvalidQuery
	}
	if len(q.Cursor) > 512 {
		return ListQuery{}, ErrInvalidCursor
	}
	return q, nil
}
