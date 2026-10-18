//go:build !windows

package daemon

import (
	"net"
	"path/filepath"
	"testing"
)

func testSocket(t *testing.T) string { return filepath.Join(t.TempDir(), "nemd.sock") }

func dialTest(path string) (net.Conn, error) { return net.Dial("unix", path) }
