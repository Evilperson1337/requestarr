package apprise

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/example/requestarr/internal/notifications"
)

type Client struct {
	BaseURL, ConfigKey string
	HTTP               *http.Client
}

func New(baseURL, configKey string) *Client {
	return &Client{BaseURL: baseURL, ConfigKey: configKey, HTTP: &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			// The Apprise key is part of the endpoint path; never forward it via redirect.
			return http.ErrUseLastResponse
		},
	}}
}
func (c *Client) Name() string { return "apprise" }

func (c *Client) Send(ctx context.Context, msg notifications.Message) error {
	base, err := url.Parse(strings.TrimRight(c.BaseURL, "/"))
	if err != nil || (base.Scheme != "https" && !(base.Scheme == "http" && (base.Hostname() == "localhost" || base.Hostname() == "127.0.0.1"))) || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return errors.New("invalid Apprise service URL")
	}
	if c.ConfigKey == "" {
		return errors.New("Apprise configuration key is required")
	}
	if msg.Title == "" || len(msg.Title) > 512 || len(msg.Body) > 2048 {
		return errors.New("invalid Apprise message")
	}
	if c.HTTP == nil {
		return errors.New("Apprise HTTP client is required")
	}
	payload, err := json.Marshal(map[string]string{"title": msg.Title, "body": msg.Body, "type": "info"})
	if err != nil {
		return err
	}
	endpoint := *base
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + "/notify/" + url.PathEscape(c.ConfigKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), strings.NewReader(string(payload)))
	if err != nil {
		return fmt.Errorf("create Apprise request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Requestarr/1.0")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return errors.New("Apprise delivery request failed")
	}
	defer resp.Body.Close()
	if _, err = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10)); err != nil {
		return errors.New("Apprise response read failed")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Apprise returned HTTP %d", resp.StatusCode)
	}
	return nil
}
