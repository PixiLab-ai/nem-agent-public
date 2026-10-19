package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// SocketPath is where nemd listens and the shell connects: a Unix socket in a directory only
// this user can open, or on Windows a named pipe only this user can open (ipc/listen_windows.go).
//
//   - Linux: $XDG_RUNTIME_DIR/nem-agent/nemd.sock (a per-user tmpfs, 0700 by spec), else
//     $TMPDIR/nem-agent-<uid>/nemd.sock;
//   - macOS: $TMPDIR/nem-agent/nemd.sock ($TMPDIR is already per user, and short enough for
//     the 104-byte socket path limit);
//   - Windows: \\.\pipe\nem-agent-<user> (Node's net module speaks named pipes, not AF_UNIX).
func SocketPath() string {
	if runtime.GOOS == "windows" {
		return `\\.\pipe\` + suffixed("nem-agent-"+pipeUser(), "")
	}
	if runtime.GOOS == "linux" {
		if d := os.Getenv("XDG_RUNTIME_DIR"); d != "" {
			return filepath.Join(d, "nem-agent", suffixed("nemd", ".sock"))
		}
		return filepath.Join(os.TempDir(), fmt.Sprintf("nem-agent-%d", os.Getuid()), suffixed("nemd", ".sock"))
	}
	return filepath.Join(os.TempDir(), "nem-agent", suffixed("nemd", ".sock"))
}

// pipeUser is the account name, reduced to characters safe in a pipe name.
func pipeUser() string {
	u := os.Getenv("USERNAME")
	if u == "" {
		u = "user"
	}
	b := []byte(u)
	for i, c := range b {
		ok := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '-' || c == '_'
		if !ok {
			b[i] = '_'
		}
	}
	return string(b)
}
