package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	maxResponseBytes = 2 << 20
	// maxAttempts bounds how often a transient provider error is retried.
	maxAttempts = 3
	// maxRetryWait caps any single wait, including one a provider asks for.
	maxRetryWait = 2 * time.Second
	// requestBudget bounds one call, retries and waits included.
	requestBudget = 8 * time.Second
)

// retryDelay is how long to wait before retry number attempt (1-based). A
// Retry-After header, in seconds, is honored up to maxRetryWait. A variable so
// tests need not sleep.
var retryDelay = func(attempt int, retryAfter string) time.Duration {
	if seconds, err := strconv.Atoi(strings.TrimSpace(retryAfter)); err == nil && seconds >= 0 {
		return min(time.Duration(seconds)*time.Second, maxRetryWait)
	}
	return min(time.Duration(attempt)*400*time.Millisecond, maxRetryWait)
}

// transient reports a status worth retrying: the provider is busy or briefly
// down (MusicBrainz answers 503 when it throttles), not rejecting the request.
func transient(status int) bool {
	return status == http.StatusTooManyRequests || status == http.StatusBadGateway || status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout
}

// Fetch GETs endpoint and decodes the JSON response into dst. It makes one
// attempt: callers with their own time budget (TMDB's optional credits) rely on that.
func Fetch(ctx context.Context, client *http.Client, endpoint string, headers http.Header, dst any) error {
	return fetch(ctx, client, http.MethodGet, endpoint, "", headers, dst, 1)
}

// FetchRetrying is Fetch that retries a transient provider error (429, 502,
// 503, 504) up to maxAttempts times, honoring a short Retry-After.
func FetchRetrying(ctx context.Context, client *http.Client, endpoint string, headers http.Header, dst any) error {
	return fetch(ctx, client, http.MethodGet, endpoint, "", headers, dst, maxAttempts)
}

// FetchWithBody POSTs body to endpoint and decodes the JSON response into dst.
func FetchWithBody(ctx context.Context, client *http.Client, endpoint, body string, headers http.Header, dst any) error {
	return fetch(ctx, client, http.MethodPost, endpoint, body, headers, dst, 1)
}

// FetchWithBodyRetrying is FetchWithBody with the retries of FetchRetrying. It
// is for read-only queries (IGDB), where resending the body is safe.
func FetchWithBodyRetrying(ctx context.Context, client *http.Client, endpoint, body string, headers http.Header, dst any) error {
	return fetch(ctx, client, http.MethodPost, endpoint, body, headers, dst, maxAttempts)
}

func fetch(ctx context.Context, client *http.Client, method, endpoint, body string, headers http.Header, dst any, attempts int) error {
	ctx, cancel := context.WithTimeout(ctx, requestBudget)
	defer cancel()
	for attempt := 1; ; attempt++ {
		resp, err := send(ctx, client, method, endpoint, body, headers)
		if err != nil {
			return err
		}
		if resp.StatusCode == http.StatusOK {
			defer resp.Body.Close()
			return decode(resp, dst)
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		if !transient(resp.StatusCode) || attempt >= attempts {
			return statusError(resp)
		}
		select {
		case <-time.After(retryDelay(attempt, resp.Header.Get("Retry-After"))):
		case <-ctx.Done():
			return fmt.Errorf("metadata provider returned %s; gave up waiting to retry: %w: %w", resp.Status, ErrBusy, ctx.Err())
		}
	}
}

// statusError describes a non-200 answer, tagged with what the caller can do about it.
func statusError(resp *http.Response) error {
	switch {
	case transient(resp.StatusCode):
		return fmt.Errorf("metadata provider returned %s: %w", resp.Status, ErrBusy)
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return fmt.Errorf("metadata provider returned %s: %w", resp.Status, ErrCredentials)
	}
	return fmt.Errorf("metadata provider returned %s", resp.Status)
}

func send(ctx context.Context, client *http.Client, method, endpoint, body string, headers http.Header) (*http.Response, error) {
	var reader io.Reader
	if method == http.MethodPost {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Requestarr/1.0 (+https://github.com/evilperson1337/requestarr)")
	for key, values := range headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("metadata provider request: %w", err)
	}
	return resp, nil
}

func decode(resp *http.Response, dst any) error {
	if !isJSON(resp.Header.Get("Content-Type")) {
		return fmt.Errorf("metadata provider returned non-JSON content")
	}
	if resp.ContentLength > maxResponseBytes {
		return fmt.Errorf("metadata provider response too large")
	}
	dec := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("decode metadata response: %w", err)
	}
	var extra json.RawMessage
	if err := dec.Decode(&extra); err != io.EOF {
		return fmt.Errorf("metadata response exceeds size limit or contains trailing data")
	}
	if resp.ContentLength < 0 && dec.InputOffset() > maxResponseBytes {
		return fmt.Errorf("metadata provider response too large")
	}
	return nil
}

func isJSON(raw string) bool {
	mediaType, _, err := mime.ParseMediaType(raw)
	return err == nil && (mediaType == "application/json" || strings.HasSuffix(mediaType, "+json"))
}
