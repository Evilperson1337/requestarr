package oidc

import (
	"bytes"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
)

func safeURL(raw string, allowHTTP bool) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	if u.Scheme == "https" {
		return true
	}
	ip := net.ParseIP(u.Hostname())
	return allowHTTP && u.Scheme == "http" && (u.Hostname() == "localhost" || (ip != nil && ip.IsLoopback()))
}

type safeTransport struct{ allowHTTP bool }

func (t safeTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if !safeURL(r.URL.String(), t.allowHTTP) {
		return nil, errors.New("unsafe OIDC endpoint")
	}
	r = r.Clone(r.Context())
	r.Header = r.Header.Clone()
	r.Header.Set("User-Agent", "Requestarr OIDC")
	resp, err := http.DefaultTransport.RoundTrip(r)
	if err != nil {
		return nil, errors.New("OIDC endpoint unavailable")
	}
	defer resp.Body.Close()
	media, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		return nil, errors.New("OIDC endpoint must return JSON")
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil || len(body) > 1<<20 {
		return nil, errors.New("OIDC endpoint response exceeds the limit or could not be read")
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	return resp, nil
}
