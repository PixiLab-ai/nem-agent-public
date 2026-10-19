// Package api is nemd's client for the Nemini API: every call goes through the Nemini site
// (Cloudflare → FWS → the API) with the device key as a bearer, exactly like the web client
// uses its cookie. Model calls never come from here — they happen on the server, the same way
// whether a message comes from the web or the app.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"time"
)

// Prefix is the API's path behind the site (FWS strips /_special/rest/Pixi).
const Prefix = "/_special/rest/Pixi/api/swarm"

// UserAgent is sent on every request: Cloudflare in front of the site refuses library defaults.
func UserAgent(version string) string {
	return fmt.Sprintf("nem-agent/%s (%s; %s)", version, runtime.GOOS, runtime.GOARCH)
}

// Error is a refused call: the HTTP status and the API's error code.
type Error struct {
	Status int
	Code   string
}

func (e *Error) Error() string {
	if e.Code == "" {
		return fmt.Sprintf("api: HTTP %d", e.Status)
	}
	return fmt.Sprintf("api: %s (HTTP %d)", e.Code, e.Status)
}

// AuthFailed — the key itself is refused (unknown, revoked or expired): pairing again is the fix.
func AuthFailed(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Status == http.StatusUnauthorized
}

// Client talks to one site with one key.
type Client struct {
	Base string // "https://nemini.ai"
	Key  string // swm_…
	UA   string
	HTTP *http.Client
}

// New returns a client with sane timeouts.
func New(base, key, ua string) *Client {
	return &Client{Base: strings.TrimRight(base, "/"), Key: key, UA: ua, HTTP: &http.Client{Timeout: 30 * time.Second}}
}

// Companion is the part of a companion the desktop needs.
type Companion struct {
	UUID string `json:"uuid"`
	Name string `json:"name"`
}

// Companions lists the companions the key can see (a pinned key sees one).
func (c *Client) Companions(ctx context.Context) ([]Companion, error) {
	var out struct {
		Companions []Companion `json:"companions"`
	}
	if err := c.do(ctx, http.MethodGet, "/companions", nil, &out); err != nil {
		return nil, err
	}
	return out.Companions, nil
}

// Pet returns the companion's pet as the API sends it (the shell renders it).
func (c *Client) Pet(ctx context.Context, companion string) (json.RawMessage, error) {
	var out json.RawMessage
	err := c.do(ctx, http.MethodGet, "/companions/"+url.PathEscape(companion)+"/pet", nil, &out)
	return out, err
}

// PetPet is a tap on the pet: `{pet, line?}`.
func (c *Client) PetPet(ctx context.Context, companion string) (json.RawMessage, error) {
	var out json.RawMessage
	err := c.do(ctx, http.MethodPost, "/companions/"+url.PathEscape(companion)+"/pet/pet", struct{}{}, &out)
	return out, err
}

// PeekAnswer is the site's answer to Peek: said = her pet answered with a line (it also arrives
// as a `pet` frame on the socket); reason when it didn't: budget · no_pet · muted · quiet · busy ·
// failed.
type PeekAnswer struct {
	Said   bool   `json:"said"`
	Line   string `json:"line,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// PetPeek tells her pet what kind of thing the human is doing (package peek's Report).
func (c *Client) PetPeek(ctx context.Context, companion string, report any) (PeekAnswer, error) {
	var out PeekAnswer
	err := c.do(ctx, http.MethodPost, "/companions/"+url.PathEscape(companion)+"/pet/peek", report, &out)
	return out, err
}

// ChatURL is the companion's chat socket, as the web client opens it (`_` carries the time
// zone so her clock speaks local time).
func (c *Client) ChatURL(companion, tz string) (string, error) {
	u, err := url.Parse(c.Base)
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	default:
		return "", fmt.Errorf("api: base %q is not http(s)", c.Base)
	}
	u.Path = Prefix + "/companions/" + companion + "/chat/ws"
	u.RawPath = Prefix + "/companions/" + url.PathEscape(companion) + "/chat/ws"
	u.RawQuery = url.Values{"_": {tz}}.Encode()
	return u.String(), nil
}

// Header is what every request and the socket upgrade carry.
func (c *Client) Header() http.Header {
	h := http.Header{}
	h.Set("Authorization", "Bearer "+c.Key)
	h.Set("User-Agent", c.UA)
	return h
}

func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Base+Prefix+path, body)
	if err != nil {
		return err
	}
	req.Header = c.Header()
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return err
	}
	return decode(res.StatusCode, raw, out)
}

// decode reads an API answer: the plain JSON body, or the FWS envelope {result, data} some
// routes are wrapped in (the web client accepts both). Failures carry the API's error code.
func decode(status int, raw []byte, out any) error {
	var env struct {
		Result  *string         `json:"result"`
		Data    json.RawMessage `json:"data"`
		Error   any             `json:"error"`
		Message string          `json:"message"`
	}
	_ = json.Unmarshal(raw, &env)
	code := ""
	switch e := env.Error.(type) {
	case string:
		code = e
	case map[string]any:
		if s, ok := e["code"].(string); ok {
			code = s
		}
	}
	if status < 200 || status > 299 {
		if code == "" && env.Message != "" {
			code = env.Message
		}
		return &Error{Status: status, Code: code}
	}
	if env.Result != nil {
		if *env.Result != "success" {
			if code == "" {
				code = env.Message
			}
			return &Error{Status: status, Code: code}
		}
		raw = env.Data
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	if p, ok := out.(*json.RawMessage); ok {
		*p = append((*p)[:0], raw...)
		return nil
	}
	return json.Unmarshal(raw, out)
}
