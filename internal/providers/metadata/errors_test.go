package metadata

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Callers tell "the provider is busy, try again" from "your credentials are
// wrong" from "that item does not exist", so the user gets a useful message.
func TestFetchClassifiesProviderFailures(t *testing.T) {
	fast(t)
	for _, tc := range []struct {
		status          int
		busy, creds, ok bool
	}{
		{http.StatusServiceUnavailable, true, false, false},
		{http.StatusTooManyRequests, true, false, false},
		{http.StatusUnauthorized, false, true, false},
		{http.StatusForbidden, false, true, false},
		{http.StatusNotFound, false, false, false},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status) }))
		var out struct{}
		err := FetchRetrying(context.Background(), server.Client(), server.URL, nil, &out)
		server.Close()
		if errors.Is(err, ErrBusy) != tc.busy || errors.Is(err, ErrCredentials) != tc.creds || err == nil {
			t.Errorf("status %d: err=%v busy=%v creds=%v", tc.status, err, errors.Is(err, ErrBusy), errors.Is(err, ErrCredentials))
		}
	}
}
