package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/PixiLab-ai/nem-agent/daemon/internal/api"
	"github.com/PixiLab-ai/nem-agent/daemon/internal/config"
	"github.com/PixiLab-ai/nem-agent/daemon/internal/desksync"
	"github.com/PixiLab-ai/nem-agent/daemon/internal/ipc"
	"github.com/PixiLab-ai/nem-agent/daemon/internal/keychain"
)

// fakeSite stands in for nemini.ai: the companion list, the pet tap, and the chat socket, all
// behind the API prefix, all requiring the bearer.
type fakeSite struct {
	t       *testing.T
	key     string
	mu      sync.Mutex
	sockets int
	send    chan string // frames to push to the open socket
	revoke  chan struct{}
}

func (f *fakeSite) handler() http.Handler {
	mux := http.NewServeMux()
	auth := func(w http.ResponseWriter, r *http.Request) bool {
		if r.Header.Get("Authorization") != "Bearer "+f.key {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"invalid_api_key"}`))
			return false
		}
		if !strings.HasPrefix(r.Header.Get("User-Agent"), "nem-agent/") {
			f.t.Errorf("request without the nem-agent User-Agent: %q", r.Header.Get("User-Agent"))
		}
		return true
	}
	mux.HandleFunc("GET "+api.Prefix+"/companions", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		_, _ = w.Write([]byte(`{"companions":[{"uuid":"c-1","name":"Nyx"}]}`))
	})
	mux.HandleFunc("POST "+api.Prefix+"/companions/c-1/pet/pet", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		// the FWS envelope some routes are wrapped in
		_, _ = w.Write([]byte(`{"result":"success","data":{"pet":{"species":"fox"},"line":"hi!"}}`))
	})
	mux.HandleFunc("GET "+api.Prefix+"/companions/c-1/chat/ws", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		f.mu.Lock()
		f.sockets++
		f.mu.Unlock()
		ctx := r.Context()
		for {
			select {
			case <-ctx.Done():
				return
			case <-f.revoke:
				_ = c.Close(websocket.StatusPolicyViolation, "key_revoked")
				return
			case s := <-f.send:
				_ = c.Write(ctx, websocket.MessageText, []byte(s))
			}
		}
	})
	return mux
}

// shell is a test IPC client: requests by id, events on a channel.
type shell struct {
	conn   net.Conn
	mu     sync.Mutex
	id     int64
	waits  map[int64]chan ipc.Response
	events chan ipc.Event
}

func dialShell(t *testing.T, path string) *shell {
	t.Helper()
	var c net.Conn
	var err error
	for i := 0; i < 50; i++ {
		if c, err = dialTest(path); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("dial nemd: %v", err)
	}
	s := &shell{conn: c, waits: map[int64]chan ipc.Response{}, events: make(chan ipc.Event, 64)}
	go func() {
		sc := bufio.NewScanner(c)
		for sc.Scan() {
			var probe map[string]json.RawMessage
			if json.Unmarshal(sc.Bytes(), &probe) != nil {
				continue
			}
			if _, isEvent := probe["event"]; isEvent {
				var ev ipc.Event
				_ = json.Unmarshal(sc.Bytes(), &ev)
				s.events <- ev
				continue
			}
			var res ipc.Response
			_ = json.Unmarshal(sc.Bytes(), &res)
			s.mu.Lock()
			ch := s.waits[res.ID]
			s.mu.Unlock()
			if ch != nil {
				ch <- res
			}
		}
	}()
	return s
}

func (s *shell) call(t *testing.T, method string, params any) ipc.Response {
	t.Helper()
	s.mu.Lock()
	s.id++
	id := s.id
	ch := make(chan ipc.Response, 1)
	s.waits[id] = ch
	s.mu.Unlock()
	raw, _ := json.Marshal(params)
	line, _ := json.Marshal(ipc.Request{ID: id, Method: method, Params: raw})
	if _, err := s.conn.Write(append(line, '\n')); err != nil {
		t.Fatalf("write: %v", err)
	}
	select {
	case r := <-ch:
		return r
	case <-time.After(5 * time.Second):
		t.Fatalf("%s: no answer", method)
	}
	return ipc.Response{}
}

// waitEvent returns the first event matching pred.
func (s *shell) waitEvent(t *testing.T, what string, pred func(ipc.Event) bool) ipc.Event {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev := <-s.events:
			if pred(ev) {
				return ev
			}
		case <-deadline:
			t.Fatalf("no %s event", what)
		}
	}
}

func socketState(state string) func(ipc.Event) bool {
	return func(ev ipc.Event) bool {
		if ev.Event != "socket" {
			return false
		}
		m, _ := ev.Data.(map[string]any)
		return m["state"] == state
	}
}

func startDaemon(t *testing.T, site *httptest.Server) (*shell, *Daemon, keychain.Store) {
	t.Helper()
	keys := keychain.NewMemory()
	cfg := &config.Config{Base: site.URL, Device: "test device"}
	d := New(cfg, "test", keys, nil)
	d.newClient = func(base, key, ua string) *api.Client {
		c := api.New(base, key, ua)
		c.HTTP = site.Client()
		return c
	}
	path := testSocket(t)
	srv, err := ipc.Listen(path, d.Handle)
	if err != nil {
		t.Fatal(err)
	}
	d.SetBroadcaster(srv)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = srv.Serve(ctx) }()
	go d.Run(ctx)
	sh := dialShell(t, path)
	if r := sh.call(t, "subscribe", nil); r.Error != nil {
		t.Fatalf("subscribe: %v", r.Error)
	}
	return sh, d, keys
}

// The whole v0 path: pair → the only companion is chosen → active → the socket opens → her
// frames reach the shell (deltas don't) → idle closes it → a revoke stops it for good.
func TestDaemonEndToEnd(t *testing.T) {
	site := &fakeSite{t: t, key: "swm_good", send: make(chan string, 8), revoke: make(chan struct{})}
	ts := httptest.NewServer(site.handler())
	defer ts.Close()
	sh, _, keys := startDaemon(t, ts)

	if r := sh.call(t, "companions", nil); r.Error == nil || r.Error.Code != "not_paired" {
		t.Fatalf("companions before pairing = %+v, want not_paired", r.Error)
	}
	if r := sh.call(t, "pair", map[string]string{"token": "swm_wrong"}); r.Error == nil || r.Error.Code != "invalid_api_key" {
		t.Fatalf("pair with a refused key = %+v, want invalid_api_key", r.Error)
	}
	if _, err := keys.Get(config.Account(ts.URL)); err != keychain.ErrNotFound {
		t.Fatalf("a refused key was stored")
	}
	r := sh.call(t, "pair", map[string]string{"token": "swm_good", "key_uuid": "k-1"})
	if r.Error != nil {
		t.Fatalf("pair: %v", r.Error)
	}
	if k, _ := keys.Get(config.Account(ts.URL)); k != "swm_good" {
		t.Fatalf("key not stored: %q", k)
	}
	if r := sh.call(t, "key", nil); r.Error != nil || r.Result.(map[string]any)["key"] != "swm_good" {
		t.Fatalf("key = %+v", r)
	}
	st := sh.call(t, "status", nil).Result.(map[string]any)
	if st["paired"] != true || st["companion"] != "c-1" || st["key_uuid"] != "k-1" {
		t.Fatalf("status after pairing = %v", st)
	}

	// not active yet: no socket
	time.Sleep(100 * time.Millisecond)
	site.mu.Lock()
	n := site.sockets
	site.mu.Unlock()
	if n != 0 {
		t.Fatalf("socket opened before the human was active")
	}

	sh.call(t, "presence.set", map[string]string{"state": "active"})
	sh.waitEvent(t, "socket open", socketState("open"))

	site.send <- `{"type":"content","delta":"hel"}`
	site.send <- `{"type":"mood","turn":"t1","face":"happy"}`
	ev := sh.waitEvent(t, "chat", func(ev ipc.Event) bool { return ev.Event == "chat" })
	if m, _ := ev.Data.(map[string]any); m["type"] != "mood" || m["face"] != "happy" {
		t.Fatalf("first forwarded frame = %v, want the mood (deltas stay in nemd)", ev.Data)
	}

	pet := sh.call(t, "pet.pet", nil)
	if pet.Error != nil {
		t.Fatalf("pet.pet: %v", pet.Error)
	}
	if m, _ := pet.Result.(map[string]any); m["line"] != "hi!" {
		t.Fatalf("pet.pet = %v", pet.Result)
	}

	sh.call(t, "presence.set", map[string]string{"state": "idle"})
	sh.waitEvent(t, "socket stopped", socketState("stopped"))

	sh.call(t, "presence.set", map[string]string{"state": "active"})
	sh.waitEvent(t, "socket open again", socketState("open"))
	close(site.revoke)
	ev = sh.waitEvent(t, "auth failed", socketState("auth_failed"))
	if m, _ := ev.Data.(map[string]any); m["reason"] != "key_revoked" {
		t.Fatalf("auth_failed reason = %v", ev.Data)
	}

	st = sh.call(t, "unpair", nil).Result.(map[string]any)
	if st["paired"] != false || st["key_uuid"] != nil {
		t.Fatalf("status after unpair = %v", st)
	}
	if r := sh.call(t, "unpair", nil); r.Error != nil {
		t.Fatalf("a second unpair failed: %v", r.Error)
	}
	if r := sh.call(t, "key", nil); r.Error == nil || r.Error.Code != "not_paired" {
		t.Fatalf("key after unpair = %+v, want not_paired", r.Error)
	}
}

func TestShutdown(t *testing.T) {
	ts := httptest.NewServer(http.NotFoundHandler())
	defer ts.Close()
	sh, d, _ := startDaemon(t, ts)
	if r := sh.call(t, "shutdown", nil); r.Error == nil || r.Error.Code != "not_allowed" {
		t.Fatalf("shutdown without a stopper = %+v, want not_allowed", r)
	}
	stopped := make(chan struct{})
	d.SetStop(func() { close(stopped) })
	if r := sh.call(t, "shutdown", nil); r.Error != nil {
		t.Fatalf("shutdown: %v", r.Error)
	}
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown didn't stop nemd")
	}
}

func TestPresenceValidated(t *testing.T) {
	ts := httptest.NewServer(http.NotFoundHandler())
	defer ts.Close()
	sh, _, _ := startDaemon(t, ts)
	if r := sh.call(t, "presence.set", map[string]string{"state": "dancing"}); r.Error == nil || r.Error.Code != "invalid_params" {
		t.Fatalf("bad presence accepted: %+v", r)
	}
	if r := sh.call(t, "nope", nil); r.Error == nil || r.Error.Code != "unknown_method" {
		t.Fatalf("unknown method = %+v", r)
	}
	if r := sh.call(t, "pair", map[string]string{"token": "not-a-key"}); r.Error == nil || r.Error.Code != "invalid_params" {
		t.Fatalf("non-swm token accepted: %+v", r)
	}
}

// desk.set marks the chosen folder and keeps it; desk.unset forgets it (the files stay); a
// non-empty folder is refused with a code the shell can say in words.
func TestDeskFolder(t *testing.T) {
	site := &fakeSite{t: t, key: "swm_good", send: make(chan string, 8), revoke: make(chan struct{})}
	ts := httptest.NewServer(site.handler())
	defer ts.Close()
	sh, d, _ := startDaemon(t, ts)
	root := t.TempDir()
	if r := sh.call(t, "desk.set", map[string]string{"root": root}); r.Error == nil || r.Error.Code != "no_companion" {
		t.Fatalf("desk.set before pairing = %+v, want no_companion", r.Error)
	}
	if r := sh.call(t, "pair", map[string]string{"token": "swm_good"}); r.Error != nil {
		t.Fatalf("pair: %v", r.Error)
	}
	if r := sh.call(t, "desk.set", map[string]string{"root": root}); r.Error != nil {
		t.Fatalf("desk.set: %v", r.Error)
	}
	if st := d.status(); st.DeskRoot != root || st.DeskCompanion != "c-1" {
		t.Fatalf("status after desk.set: %+v", st)
	}
	if _, err := os.Stat(filepath.Join(root, desksync.MarkerName)); err != nil {
		t.Fatalf("no marker: %v", err)
	}
	if r := sh.call(t, "desk.unset", nil); r.Error != nil {
		t.Fatalf("desk.unset: %v", r.Error)
	}
	if st := d.status(); st.DeskRoot != "" {
		t.Fatalf("still set: %+v", st)
	}
	if _, err := os.Stat(filepath.Join(root, desksync.MarkerName)); err != nil {
		t.Fatalf("desk.unset removed the folder's marker: %v", err)
	}
	full := t.TempDir()
	if err := os.WriteFile(filepath.Join(full, "a.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if r := sh.call(t, "desk.set", map[string]string{"root": full}); r.Error == nil || r.Error.Code != "not_empty" {
		t.Fatalf("a full folder = %+v, want not_empty", r.Error)
	}
	// signing out stops the folder sync
	if r := sh.call(t, "desk.set", map[string]string{"root": root}); r.Error != nil {
		t.Fatalf("desk.set again (re-link): %v", r.Error)
	}
	if r := sh.call(t, "unpair", nil); r.Error != nil {
		t.Fatalf("unpair: %v", r.Error)
	}
	if st := d.status(); st.DeskRoot != "" {
		t.Fatalf("unpair kept the folder: %+v", st)
	}
}
