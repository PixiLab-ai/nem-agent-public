// Package ipc is nemd's private channel to the desktop shell: newline-delimited JSON over a Unix
// socket in a directory only this user can open (listen_unix.go), or on Windows a named pipe only
// this user can open (listen_windows.go). Nothing listens on a network port.
//
// The shell sends requests and gets responses by id; a connection that called Subscribe also
// receives events, pushed whenever they happen.
//
//	→ {"id":1,"method":"status"}
//	← {"id":1,"result":{...}}
//	← {"id":2,"error":{"code":"not_paired","message":"..."}}
//	← {"event":"chat","data":{"type":"mood","face":"happy"}}
package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// Request is one call from the shell.
type Request struct {
	ID     int64           `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// Response answers a request by id: Result or Error.
type Response struct {
	ID     int64  `json:"id"`
	Result any    `json:"result,omitempty"`
	Error  *Error `json:"error,omitempty"`
}

// Error is a coded failure the shell can act on.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

// Errorf builds a coded error for a handler to return.
func Errorf(code, format string, a ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, a...)}
}

// Event is pushed to subscribed connections.
type Event struct {
	Event string `json:"event"`
	Data  any    `json:"data,omitempty"`
}

// Handler serves one request. A returned *Error keeps its code; any other error becomes
// "internal".
type Handler func(ctx context.Context, c *Conn, method string, params json.RawMessage) (any, error)

// Server accepts shell connections.
type Server struct {
	ln      net.Listener
	path    string
	handler Handler

	mu    sync.Mutex
	conns map[*Conn]struct{}
}

// maxLine bounds one message (a device key, a frame, a list of companions — all small).
const maxLine = 1 << 20

// Listen creates the socket. The parent directory is made 0700 and the socket 0600; a stale
// socket left by a crashed nemd is removed, a live one is an error (one nemd per user).
func Listen(path string, h Handler) (*Server, error) {
	ln, err := listen(path)
	if err != nil {
		return nil, err
	}
	return &Server{ln: ln, path: path, handler: h, conns: map[*Conn]struct{}{}}, nil
}

// Path is the socket path.
func (s *Server) Path() string { return s.path }

// Serve accepts connections until ctx ends, then closes everything and removes the socket.
func (s *Server) Serve(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		_ = s.ln.Close()
		s.mu.Lock()
		for c := range s.conns {
			_ = c.nc.Close()
		}
		s.mu.Unlock()
	}()
	defer cleanup(s.path)
	for {
		nc, err := s.ln.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		c := &Conn{nc: nc}
		s.mu.Lock()
		s.conns[c] = struct{}{}
		s.mu.Unlock()
		go s.serveConn(ctx, c)
	}
}

func (s *Server) serveConn(ctx context.Context, c *Conn) {
	defer func() {
		s.mu.Lock()
		delete(s.conns, c)
		s.mu.Unlock()
		_ = c.nc.Close()
	}()
	sc := bufio.NewScanner(c.nc)
	sc.Buffer(make([]byte, 0, 64<<10), maxLine)
	for sc.Scan() {
		var req Request
		if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
			_ = c.write(Response{Error: Errorf("bad_request", "not a JSON request")})
			continue
		}
		go func(req Request) {
			res, err := s.handler(ctx, c, req.Method, req.Params)
			out := Response{ID: req.ID, Result: res}
			if err != nil {
				var e *Error
				if !errors.As(err, &e) {
					e = &Error{Code: "internal", Message: err.Error()}
				}
				out = Response{ID: req.ID, Error: e}
			}
			_ = c.write(out)
		}(req)
	}
}

// Broadcast pushes an event to every subscribed connection.
func (s *Server) Broadcast(ev Event) {
	s.mu.Lock()
	targets := make([]*Conn, 0, len(s.conns))
	for c := range s.conns {
		if c.subscribed.Load() {
			targets = append(targets, c)
		}
	}
	s.mu.Unlock()
	for _, c := range targets {
		_ = c.write(ev)
	}
}

// Conn is one shell connection.
type Conn struct {
	nc         net.Conn
	wmu        sync.Mutex
	subscribed atomic.Bool
}

// Subscribe makes this connection receive events.
func (c *Conn) Subscribe() { c.subscribed.Store(true) }

func (c *Conn) write(v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	c.wmu.Lock()
	defer c.wmu.Unlock()
	_ = c.nc.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_, err = c.nc.Write(append(raw, '\n'))
	return err
}
