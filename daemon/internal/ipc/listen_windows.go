//go:build windows

package ipc

import (
	"fmt"
	"net"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

// listen opens the named pipe with a DACL that names this user and nobody else: no Everyone, no
// other accounts on the machine. The first pipe instance is created exclusively
// (FILE_FLAG_FIRST_PIPE_INSTANCE), so a second nemd fails here instead of sharing the name.
func listen(path string) (net.Listener, error) {
	sid, err := currentUserSID()
	if err != nil {
		return nil, fmt.Errorf("nemd pipe: the current user's SID: %w", err)
	}
	ln, err := winio.ListenPipe(path, &winio.PipeConfig{
		SecurityDescriptor: "D:P(A;;GA;;;" + sid + ")",
		InputBufferSize:    64 << 10,
		OutputBufferSize:   64 << 10,
	})
	if err != nil {
		return nil, fmt.Errorf("nemd pipe %s: %w (is nemd already running?)", path, err)
	}
	return ln, nil
}

// cleanup: a named pipe goes away with its last handle.
func cleanup(string) {}

func currentUserSID() (string, error) {
	u, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "", err
	}
	return u.User.Sid.String(), nil
}
