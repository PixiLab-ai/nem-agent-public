package config

import (
	"path/filepath"
	"strings"
	"testing"
)

// The development app runs under a profile so it never touches the installed app's config or
// socket (a dev run once left the installed app on the dev site).
func TestProfileSeparatesState(t *testing.T) {
	t.Cleanup(func() { _ = SetProfile("") })
	def, _ := file()
	defSock := SocketPath()
	if err := SetProfile("dev"); err != nil {
		t.Fatal(err)
	}
	dev, _ := file()
	devSock := SocketPath()
	if dev == def || devSock == defSock {
		t.Fatalf("the dev profile shares state: %s / %s", dev, devSock)
	}
	if filepath.Base(dev) != "nemd-dev.json" || !strings.Contains(filepath.Base(devSock), "-dev") {
		t.Fatalf("dev names: %s, %s", dev, devSock)
	}
	if err := SetProfile("Dev/../x"); err == nil {
		t.Fatal("a profile with path characters was accepted")
	}
}
