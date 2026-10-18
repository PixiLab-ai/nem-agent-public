// Package chat holds the companion's chat socket open for the desktop: the bearer on the
// upgrade, JSON frames handed to a callback, a ping every 30 s, reconnects with backoff, and a
// clean stop when the key itself is refused (unknown, revoked, expired) — pairing again is the
// only fix for that, so retrying would just knock on the door forever.
package chat

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math/rand/v2"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
)

// State is the socket's lifecycle as the shell sees it.
type State string

const (
	Connecting State = "connecting"
	Open       State = "open"
	Retrying   State = "retrying"    // dropped or refused for a passing reason; trying again
	AuthFailed State = "auth_failed" // the key is refused: stop, the human must pair again
	Gone       State = "gone"        // the companion isn't there for this key (404): stop
	Stopped    State = "stopped"     // ctx ended
)

// Options configures one socket run.
type Options struct {
	URL    string
	Header http.Header
	// Frame receives each JSON text frame. Binary frames (her voice) are dropped: the desktop
	// overlay doesn't play audio.
	Frame func(map[string]any)
	// State receives lifecycle changes, with a short reason ("invalid_api_key", "key_revoked"…).
	State func(State, string)
	// PingEvery defaults to 30 s, the web client's interval.
	PingEvery time.Duration
	// Dial overrides the dial (tests); nil = websocket.Dial.
	Dial func(ctx context.Context, url string, opts *websocket.DialOptions) (*websocket.Conn, *http.Response, error)
}

// Run keeps the socket open until ctx ends or the key / companion is refused.
func Run(ctx context.Context, o Options) {
	if o.PingEvery <= 0 {
		o.PingEvery = 30 * time.Second
	}
	if o.Dial == nil {
		o.Dial = websocket.Dial
	}
	state := func(s State, why string) {
		if o.State != nil {
			o.State(s, why)
		}
	}
	retry := 0
	for {
		if ctx.Err() != nil {
			state(Stopped, "")
			return
		}
		state(Connecting, "")
		stop, why := once(ctx, o, func() { retry = 0; state(Open, "") })
		switch {
		case ctx.Err() != nil:
			state(Stopped, "")
			return
		case stop == AuthFailed || stop == Gone:
			state(stop, why)
			return
		}
		state(Retrying, why)
		// 1 s, 2, 4 … capped at 30 s, jittered ×0.5–1.5 — the web client's schedule.
		d := time.Duration(float64(min(30*time.Second, time.Second<<min(retry, 5))) * (0.5 + rand.Float64()))
		retry++
		select {
		case <-ctx.Done():
			state(Stopped, "")
			return
		case <-time.After(d):
		}
	}
}

// once is one connection: it returns why it ended (AuthFailed / Gone stop for good, anything
// else is retried) and a short reason.
func once(ctx context.Context, o Options, opened func()) (State, string) {
	dctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	conn, res, err := o.Dial(dctx, o.URL, &websocket.DialOptions{HTTPHeader: o.Header.Clone()})
	cancel()
	if err != nil {
		return refusal(res, err)
	}
	defer conn.CloseNow()
	conn.SetReadLimit(4 << 20)
	opened()

	pctx, pcancel := context.WithCancel(ctx)
	defer pcancel()
	go func() {
		t := time.NewTicker(o.PingEvery)
		defer t.Stop()
		for {
			select {
			case <-pctx.Done():
				return
			case <-t.C:
				wctx, wcancel := context.WithTimeout(pctx, 10*time.Second)
				_ = conn.Write(wctx, websocket.MessageText, []byte(`{"type":"ping"}`))
				wcancel()
			}
		}
	}()

	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return Stopped, ""
			}
			return closed(err)
		}
		if typ != websocket.MessageText {
			continue
		}
		var f map[string]any
		if json.Unmarshal(data, &f) != nil {
			continue
		}
		if t, _ := f["type"].(string); t == "pong" {
			continue
		}
		if o.Frame != nil {
			o.Frame(f)
		}
	}
}

// refusal classifies a failed upgrade. The server answers auth errors as JSON before the
// upgrade: 401 = the key is refused (stop), 404 = no such companion for this key (stop),
// anything else is passing.
func refusal(res *http.Response, err error) (State, string) {
	if res == nil {
		return Retrying, shortErr(err)
	}
	code := ""
	if res.Body != nil {
		raw, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
		var b struct {
			Error any `json:"error"`
		}
		if json.Unmarshal(raw, &b) == nil {
			if s, ok := b.Error.(string); ok {
				code = s
			}
		}
	}
	switch res.StatusCode {
	case http.StatusUnauthorized:
		if code == "" {
			code = "unauthorized"
		}
		return AuthFailed, code
	case http.StatusNotFound:
		if code == "" {
			code = "not_found"
		}
		return Gone, code
	}
	if code == "" {
		code = res.Status
	}
	return Retrying, code
}

// closed classifies a dropped connection. The server closes a socket whose key was revoked or
// expired with 1008 (policy violation) and the reason key_revoked / key_expired.
func closed(err error) (State, string) {
	var ce websocket.CloseError
	if errors.As(err, &ce) {
		if ce.Code == websocket.StatusPolicyViolation {
			r := ce.Reason
			if r == "" {
				r = "policy_violation"
			}
			return AuthFailed, r
		}
		return Retrying, strings.TrimSpace(ce.Code.String() + " " + ce.Reason)
	}
	return Retrying, shortErr(err)
}

func shortErr(err error) string {
	s := err.Error()
	if len(s) > 160 {
		s = s[:160]
	}
	return s
}
