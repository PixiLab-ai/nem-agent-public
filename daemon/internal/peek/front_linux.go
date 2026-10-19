//go:build linux

package peek

// The app in front on Linux. On X11: the active window (_NET_ACTIVE_WINDOW), its class
// (WM_CLASS — which app it is, not its title) and its process (_NET_WM_PID → /proc: the program's
// path, to tell a game; a Proton game's Windows path lives in its command line, read only for
// Wine/Proton programs). A Wayland session doesn't say which app is in front: there Peek only
// notices what's running — a game, a meeting app.

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

// NewSampler picks what this session can tell.
func NewSampler() (Sampler, string) {
	if os.Getenv("XDG_SESSION_TYPE") != "wayland" && os.Getenv("DISPLAY") != "" {
		if s, err := newX11(); err == nil {
			return s, ModeFront
		}
	}
	return &procSampler{}, ModeRunning
}

type x11Sampler struct {
	mu                       sync.Mutex
	conn                     *xgb.Conn
	root                     xproto.Window
	active, pid, class, wmCl xproto.Atom
	state, full              xproto.Atom // _NET_WM_STATE, _NET_WM_STATE_FULLSCREEN (0: the WM doesn't say)
}

func newX11() (*x11Sampler, error) {
	s := &x11Sampler{}
	return s, s.connect()
}

func (s *x11Sampler) connect() error {
	conn, err := xgb.NewConn()
	if err != nil {
		return err
	}
	atom := func(name string) (xproto.Atom, error) {
		r, err := xproto.InternAtom(conn, true, uint16(len(name)), name).Reply()
		if err != nil {
			return 0, err
		}
		return r.Atom, nil
	}
	var e1, e2, e3 error
	s.active, e1 = atom("_NET_ACTIVE_WINDOW")
	s.pid, e2 = atom("_NET_WM_PID")
	s.wmCl, e3 = atom("WM_CLASS")
	if err := errors.Join(e1, e2, e3); err != nil || s.active == 0 {
		conn.Close()
		return errors.New("peek: no EWMH window manager")
	}
	s.state, _ = atom("_NET_WM_STATE")
	s.full, _ = atom("_NET_WM_STATE_FULLSCREEN")
	s.conn = conn
	s.root = xproto.Setup(conn).DefaultScreen(conn).Root
	return nil
}

func (s *x11Sampler) Front() (Front, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn == nil {
		if err := s.connect(); err != nil {
			return Front{}, err
		}
	}
	f, err := s.front()
	if err != nil {
		s.conn.Close() // reconnect on the next sample (the X server restarted…)
		s.conn = nil
	}
	return f, err
}

func (s *x11Sampler) front() (Front, error) {
	r, err := xproto.GetProperty(s.conn, false, s.root, s.active, xproto.AtomWindow, 0, 1).Reply()
	if err != nil {
		return Front{}, err
	}
	if len(r.Value) < 4 {
		return Front{}, nil
	}
	win := xproto.Window(xgb.Get32(r.Value))
	if win == 0 {
		return Front{}, nil
	}
	var f Front
	if c, err := xproto.GetProperty(s.conn, false, win, s.wmCl, xproto.AtomString, 0, 64).Reply(); err == nil {
		parts := bytes.Split(bytes.TrimRight(c.Value, "\x00"), []byte{0})
		if len(parts) > 0 {
			f.Instance = string(parts[0])
		}
		if len(parts) > 1 {
			f.Class = string(parts[1])
		}
	}
	if s.pid != 0 {
		if p, err := xproto.GetProperty(s.conn, false, win, s.pid, xproto.AtomCardinal, 0, 1).Reply(); err == nil && len(p.Value) >= 4 {
			pid := int(xgb.Get32(p.Value))
			f.Exe, f.Cmdline = procInfo(pid, f.Class)
		}
	}
	// full screen (a game, a film, a presentation): the pet keeps quiet
	if s.state != 0 && s.full != 0 {
		if st, err := xproto.GetProperty(s.conn, false, win, s.state, xproto.AtomAtom, 0, 32).Reply(); err == nil {
			for i := 0; i+4 <= len(st.Value); i += 4 {
				if xproto.Atom(xgb.Get32(st.Value[i:])) == s.full {
					f.Quiet = true
				}
			}
		}
	}
	return f, nil
}

func (s *x11Sampler) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn != nil {
		s.conn.Close()
		s.conn = nil
	}
}

// procInfo is a process's program path and, for a Wine/Proton program only (a Windows game's
// path is in its command line), the command line.
func procInfo(pid int, class string) (exe, cmdline string) {
	if pid <= 0 {
		return "", ""
	}
	dir := "/proc/" + strconv.Itoa(pid)
	exe, _ = os.Readlink(dir + "/exe")
	base := strings.ToLower(filepath.Base(exe))
	if strings.HasPrefix(base, "wine") || strings.Contains(strings.ToLower(exe), "proton") || strings.HasSuffix(strings.ToLower(class), ".exe") {
		if raw, err := os.ReadFile(dir + "/cmdline"); err == nil {
			cmdline = strings.ReplaceAll(string(raw), "\x00", " ")
		}
	}
	return exe, cmdline
}

// procSampler: a Wayland session — what's running, not what's in front: a game first, then a
// meeting app; nothing else.
type procSampler struct{}

func (procSampler) Front() (Front, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return Front{}, err
	}
	var meeting Front
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		exe, cmd := procInfo(pid, "")
		if exe == "" {
			continue
		}
		f := Front{Exe: exe, Cmdline: cmd}
		switch Categorize(f).Category {
		case Gaming:
			if !isLauncher(exe) {
				return f, nil
			}
		case Meeting:
			meeting = f
		}
	}
	return meeting, nil
}

func (procSampler) Close() {}

// isLauncher — a game launcher idling in the background (Steam, Lutris…) isn't a game being played.
func isLauncher(exe string) bool {
	switch programName(exe) {
	case "steam", "steamwebhelper", "lutris", "heroic", "galaxyclient", "epicgameslauncher", "battle.net", "riotclientservices", "reaper", "pressure-vessel-wrap", "srt-bwrap":
		return true
	}
	return false
}
