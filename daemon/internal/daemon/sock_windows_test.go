//go:build windows

package daemon

import (
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/Microsoft/go-winio"
)

func testSocket(t *testing.T) string {
	return fmt.Sprintf(`\\.\pipe\nemd-test-%d`, time.Now().UnixNano())
}

func dialTest(path string) (net.Conn, error) {
	timeout := time.Second
	return winio.DialPipe(path, &timeout)
}
