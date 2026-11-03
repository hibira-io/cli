package pusher

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Credentials identify one app on one Hibira host.
type Credentials struct {
	AppID  string
	Key    string
	Secret string
	Host   string // host name, with an optional :port
	Scheme string // "https" (default) or "http"
}

// BaseURL is the HTTP API root, such as https://ws.hibira.io.
func (c Credentials) BaseURL() string {
	scheme := c.Scheme
	if scheme == "" {
		scheme = "https"
	}
	return scheme + "://" + c.Host
}

// Client calls the Pusher-compatible HTTP API.
type Client struct {
	Creds     Credentials
	HTTP      *http.Client
	UserAgent string
	now       func() time.Time
}

// NewClient returns a client with a sensible timeout.
func NewClient(c Credentials, userAgent string) *Client {
	return &Client{Creds: c, HTTP: &http.Client{Timeout: 15 * time.Second}, UserAgent: userAgent, now: time.Now}
}

// TriggerParams is the body of POST /apps/{id}/events.
type TriggerParams struct {
	Name     string   `json:"name"`
	Channels []string `json:"channels"`
	Data     string   `json:"data"`
	SocketID string   `json:"socket_id,omitempty"`
	Info     string   `json:"info,omitempty"`
}

// APIError is a non-2xx answer from the HTTP API.
type APIError struct {
	Status int
	Body   string
}

func (e *APIError) Error() string {
	msg := strings.TrimSpace(e.Body)
	if msg == "" {
		msg = http.StatusText(e.Status)
	}
	return fmt.Sprintf("HTTP %d: %s", e.Status, msg)
}

// Trigger publishes one event and returns the raw JSON answer.
func (c *Client) Trigger(ctx context.Context, p TriggerParams) (json.RawMessage, error) {
	if p.Name == "" {
		return nil, errors.New("event name is required")
	}
	if len(p.Channels) == 0 {
		return nil, errors.New("at least one channel is required")
	}
	body, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	return c.do(ctx, http.MethodPost, "/apps/"+c.Creds.AppID+"/events", nil, body)
}

// Channels lists occupied channels, optionally filtered by prefix.
func (c *Client) Channels(ctx context.Context, prefix string, info string) (json.RawMessage, error) {
	q := url.Values{}
	if prefix != "" {
		q.Set("filter_by_prefix", prefix)
	}
	if info != "" {
		q.Set("info", info)
	}
	return c.do(ctx, http.MethodGet, "/apps/"+c.Creds.AppID+"/channels", q, nil)
}

func (c *Client) do(ctx context.Context, method, path string, q url.Values, body []byte) (json.RawMessage, error) {
	if c.Creds.AppID == "" || c.Creds.Key == "" || c.Creds.Secret == "" {
		return nil, errors.New("app id, key and secret are all required for the HTTP API")
	}
	signed := signedQuery(c.Creds.Key, c.Creds.Secret, method, path, q, body, c.now())
	u := c.Creds.BaseURL() + path + "?" + signed.Encode()
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, &APIError{Status: resp.StatusCode, Body: string(out)}
	}
	if len(bytes.TrimSpace(out)) == 0 {
		out = []byte("{}")
	}
	return out, nil
}
