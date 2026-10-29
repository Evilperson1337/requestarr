package checks

import (
	"context"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/evilperson1337/requestarr/internal/devlogs"
)

// fetchSpec describes one connection-check request and what a healthy answer
// looks like. Every probe (library Sources, Apprise, TMDB, IGDB, MusicBrainz,
// OIDC discovery) goes through fetch, so status, content type, size and body
// are judged in one place and recorded as one diagnostic.
type fetchSpec struct {
	stage    devlogs.Stage
	method   string
	endpoint string
	body     io.Reader
	headers  map[string]string
	// accept is the Accept header; empty sends none.
	accept string
	// trusted allows the private-address dial exception for an endpoint the
	// operator configured (see safeTransport).
	trusted bool
	// types are the acceptable response media types; nil accepts any.
	types []string
	// maxBytes bounds the body read.
	maxBytes int64
	// discard drains and ignores the body (a bare liveness probe).
	discard bool
	// validate judges the body; nil accepts any non-empty body.
	validate func([]byte) bool
}

// fetch performs the request. It returns errAuthentication for a 401 or 403,
// errCheck for any other failure, and nil when the answer is healthy. It never
// returns or logs the response.
func (c *Checker) fetch(ctx context.Context, spec fetchSpec) error {
	probe := c.probe(ctx, spec.stage)
	defer probe.finish()
	if spec.trusted {
		ctx = context.WithValue(ctx, trustedDialKey{}, true)
	}
	req, err := http.NewRequestWithContext(ctx, spec.method, spec.endpoint, spec.body)
	if err != nil {
		return errCheck
	}
	req.Header.Set("User-Agent", "Requestarr/setup-check")
	if spec.accept != "" {
		req.Header.Set("Accept", spec.accept)
	}
	for key, value := range spec.headers {
		req.Header.Set(key, value)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		probe.network(err)
		return errCheck
	}
	defer resp.Body.Close()
	probe.status = resp.StatusCode
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		probe.code = devlogs.CodeHTTPStatus
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return errAuthentication
		}
		return errCheck
	}
	if spec.discard {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, spec.maxBytes))
		probe.code = devlogs.CodeConnected
		return nil
	}
	if spec.types != nil {
		mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
		if err != nil || !contains(spec.types, mediaType) {
			probe.code = devlogs.CodeContentType
			return errCheck
		}
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, spec.maxBytes+1))
	if err != nil {
		probe.network(err)
		return errCheck
	}
	if int64(len(data)) > spec.maxBytes {
		probe.code = devlogs.CodeResponseSize
		return errCheck
	}
	if len(data) == 0 || spec.validate != nil && !spec.validate(data) {
		return errCheck // the diagnostic keeps its "malformed" default
	}
	probe.code = devlogs.CodeConnected
	return nil
}

func contains(list []string, value string) bool {
	for _, item := range list {
		if strings.EqualFold(item, value) {
			return true
		}
	}
	return false
}
