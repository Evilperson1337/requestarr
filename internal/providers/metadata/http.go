package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"
)

const maxResponseBytes = 2 << 20

func Fetch(ctx context.Context, client *http.Client, endpoint string, headers http.Header, dst any) error {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Requestarr/1.0 (+https://github.com/example/requestarr)")
	for key, values := range headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("metadata provider request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("metadata provider returned %s", resp.Status)
	}
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

func FetchWithBody(ctx context.Context, client *http.Client, endpoint, body string, headers http.Header, dst any) error {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Requestarr/1.0 (+https://github.com/example/requestarr)")
	for key, values := range headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("metadata provider request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("metadata provider returned %s", resp.Status)
	}
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
