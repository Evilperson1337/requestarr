package apprise

import (
	"context"
	"errors"
	"net/http"
	"time"

	"git.example.com/evilperson1337/Requestarr/internal/notifications"
)

type Client struct {
	BaseURL   string
	ConfigKey string
	HTTP      *http.Client
}

func New(baseURL, configKey string) *Client {
	return &Client{BaseURL: baseURL, ConfigKey: configKey, HTTP: &http.Client{Timeout: 10 * time.Second}}
}

func (c *Client) Name() string { return "apprise" }

func (c *Client) Send(ctx context.Context, msg notifications.Message) error {
	// Intentionally not hard-coded in the scaffold: implement against the current
	// Apprise API contract and cover stateful/config-key and optional stateless modes.
	// See docs/INTEGRATIONS.md and the upstream reference manifest.
	if c.BaseURL == "" {
		return errors.New("apprise base URL is required")
	}
	return errors.New("apprise sender not implemented")
}
