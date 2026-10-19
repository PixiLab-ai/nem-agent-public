// nemd is nem-agent's daemon: it holds the device key (in the OS keychain), talks to the Nemini
// API through the site, and serves the desktop shell over a private Unix socket. It only dials
// out; nothing listens on a network port.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/PixiLab-ai/nem-agent/daemon/internal/config"
	"github.com/PixiLab-ai/nem-agent/daemon/internal/daemon"
	"github.com/PixiLab-ai/nem-agent/daemon/internal/ipc"
	"github.com/PixiLab-ai/nem-agent/daemon/internal/keychain"
)

// version is stamped at build time (-ldflags "-X main.version=…").
var version = "0.0.0-dev"

func main() {
	socket := flag.String("socket", "", "IPC socket path (default: a per-user runtime directory)")
	base := flag.String("base", "", "Nemini site, e.g. https://nemini.ai (default: the saved one)")
	ephemeral := flag.Bool("ephemeral", false, "keep the key in memory only and never write the config (tests, demos)")
	printSocket := flag.Bool("print-socket", false, "print the default socket path and exit")
	showVersion := flag.Bool("version", false, "print the version and exit")
	prof := flag.String("profile", "", `a separate config and socket ("dev" for development runs), so they never touch the installed app's`)
	flag.Parse()
	if err := config.SetProfile(*prof); err != nil {
		log.Fatalf("nemd: --profile: %v", err)
	}

	if *showVersion {
		fmt.Println(version)
		return
	}
	path := *socket
	if path == "" {
		path = config.SocketPath()
	}
	if *printSocket {
		fmt.Println(path)
		return
	}

	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("nemd: %v", err)
	}
	if *base != "" {
		b, err := config.ValidBase(*base)
		if err != nil {
			log.Fatalf("nemd: --base: %v", err)
		}
		cfg.Base = b
	}

	var keys keychain.Store = keychain.System{}
	save := daemon.Saver(func(c *config.Config) error { return c.Save() })
	if *ephemeral {
		keys = keychain.NewMemory()
		save = nil
	}

	d := daemon.New(cfg, version, keys, save)
	srv, err := ipc.Listen(path, d.Handle)
	if err != nil {
		log.Fatalf("nemd: %v", err)
	}
	d.SetBroadcaster(srv)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	d.SetStop(stop)
	log.Printf("nemd %s: %s, listening on %s", version, cfg.Base, srv.Path())
	go func() {
		if err := srv.Serve(ctx); err != nil {
			log.Printf("nemd: ipc: %v", err)
			stop()
		}
	}()
	d.Run(ctx)
	log.Printf("nemd: stopped")
}
