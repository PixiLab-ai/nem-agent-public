// Package daemon is nemd's core: it holds the device key, decides when the chat socket runs, and
// serves the shell's requests over IPC. The shell's main window is the Nemini web app signed in
// with this key only (no cookie): the shell asks for it (`key`) to add it to the window's API
// calls. The IPC socket is private to the user (0600 in a 0700 directory).
//
// The chat socket is open only while three things are true: the device is paired, a companion
// is chosen, and the shell says the human is active. Any open socket counts as "the human is
// here" on the server (it mutes web push and starts the pet's bubble loop), so an app left open
// all day must not hold one while nobody's at the machine.
package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/PixiLab-ai/nem-agent/daemon/internal/api"
	"github.com/PixiLab-ai/nem-agent/daemon/internal/chat"
	"github.com/PixiLab-ai/nem-agent/daemon/internal/config"
	"github.com/PixiLab-ai/nem-agent/daemon/internal/desksync"
	"github.com/PixiLab-ai/nem-agent/daemon/internal/ipc"
	"github.com/PixiLab-ai/nem-agent/daemon/internal/keychain"
)

// Broadcaster pushes events to the shell.
type Broadcaster interface{ Broadcast(ipc.Event) }

// Saver persists the config (a test can drop it).
type Saver func(*config.Config) error

// Presence values the shell reports.
const (
	Active = "active"
	Idle   = "idle"
	Away   = "away"
	Locked = "locked"
)

// forwarded are the chat frames the shell gets. Streaming deltas, tool frames and audio stay
// here: the overlay needs moments (she's writing, she replied, her face changed), not text.
var forwarded = map[string]bool{
	"turn_start": true, "turn_end": true, "turn_idle": true, "running": true,
	"mood": true, "pet": true, "xp": true, "achievement": true, "desk_full": true,
	"desktop": true, "wiped": true, "reaction": true,
}

// Daemon is the state behind the IPC methods.
type Daemon struct {
	version string
	keys    keychain.Store
	save    Saver
	bc      Broadcaster
	stop    func() // ends nemd (the `shutdown` method); nil = refused
	tz      string

	// newClient builds the API client (tests point it at a fake site).
	newClient func(base, key, ua string) *api.Client
	dial      func(context.Context, chat.Options)

	mu       sync.Mutex
	cfg      *config.Config
	presence string
	sock     context.CancelFunc
	sockKey  string // what the running socket was opened for: base|companion
	sockSt   chat.State
	sockWhy  string
	root     context.Context

	// her desktop's folder (package desksync): the running engine, what it was started for, its news
	desk       *desksync.Engine
	deskCancel context.CancelFunc
	deskID     string
	deskSt     *desksync.Status
}

// New builds a daemon. Run must be called before requests are served.
func New(cfg *config.Config, version string, keys keychain.Store, save Saver) *Daemon {
	tz := time.Local.String()
	if tz == "Local" {
		tz = ""
	}
	return &Daemon{
		version: version, keys: keys, save: save, tz: tz, cfg: cfg,
		newClient: api.New, dial: chat.Run, sockSt: chat.Stopped,
	}
}

// SetBroadcaster wires the IPC server's event push.
func (d *Daemon) SetBroadcaster(b Broadcaster) { d.bc = b }

// SetStop lets the shell end nemd (`shutdown`): an updated app replaces a running nemd of
// another version with its own.
func (d *Daemon) SetStop(stop func()) { d.stop = stop }

// Run keeps the daemon alive until ctx ends.
func (d *Daemon) Run(ctx context.Context) {
	d.mu.Lock()
	d.root = ctx
	d.mu.Unlock()
	d.reconcile()
	<-ctx.Done()
	d.mu.Lock()
	if d.sock != nil {
		d.sock()
		d.sock = nil
	}
	d.mu.Unlock()
}

func (d *Daemon) emit(event string, data any) {
	if d.bc != nil {
		d.bc.Broadcast(ipc.Event{Event: event, Data: data})
	}
}

func (d *Daemon) ua() string { return api.UserAgent(d.version) }

// key returns the stored device key for the current site ("" = not paired).
func (d *Daemon) key(base string) (string, error) {
	k, err := d.keys.Get(config.Account(base))
	if errors.Is(err, keychain.ErrNotFound) {
		return "", nil
	}
	return k, err
}

// client returns an API client for the current site, or a coded error when not paired.
func (d *Daemon) client() (*api.Client, *config.Config, error) {
	d.mu.Lock()
	cfg := *d.cfg
	d.mu.Unlock()
	k, err := d.key(cfg.Base)
	if err != nil {
		return nil, nil, ipc.Errorf("keychain_unavailable", "%v", err)
	}
	if k == "" {
		return nil, nil, ipc.Errorf("not_paired", "this computer isn't paired with %s yet", cfg.Base)
	}
	return d.newClient(cfg.Base, k, d.ua()), &cfg, nil
}

// reconcile brings the chat socket and the folder sync in line with the state.
func (d *Daemon) reconcile() {
	d.reconcileSocket()
	d.reconcileDesk()
}

// reconcileDesk runs her desktop's folder sync while a folder is set and the computer is paired
// with the site it was set on; anything else stops it (its files stay).
func (d *Daemon) reconcileDesk() {
	d.mu.Lock()
	if d.root == nil {
		d.mu.Unlock()
		return
	}
	cfg := *d.cfg
	d.mu.Unlock()
	k, _ := d.key(cfg.Base)
	want := cfg.DeskRoot != "" && cfg.DeskCompanion != "" && k != ""
	id := cfg.DeskRoot + "|" + cfg.DeskCompanion + "|" + cfg.Base + "|" + k
	d.mu.Lock()
	if want && d.deskID == id {
		d.mu.Unlock()
		return
	}
	if d.deskCancel != nil {
		d.deskCancel()
	}
	d.desk, d.deskCancel, d.deskID, d.deskSt = nil, nil, "", nil
	root := d.root
	d.mu.Unlock()
	if !want {
		return
	}
	stateFile, err := config.StateFile("desk-"+cfg.DeskCompanion, ".json")
	if err != nil {
		d.setDeskStatus(desksync.Status{State: "error", Detail: "config_dir"})
		return
	}
	trash, _ := config.StateFile("desk-trash-"+cfg.DeskCompanion, "")
	desksync.PruneTrash(trash, 30*24*time.Hour)
	st, err := desksync.LoadState(stateFile, cfg.DeskCompanion, cfg.DeskRoot)
	if err != nil {
		d.setDeskStatus(desksync.Status{State: "error", Detail: "state_unreadable"})
		return
	}
	dl := &http.Client{Timeout: 10 * time.Minute}
	ua := d.ua()
	eng := desksync.New(desksync.Options{
		Root: cfg.DeskRoot, Companion: cfg.DeskCompanion, Device: cfg.Device,
		Remote: d.newClient(cfg.Base, k, ua),
		Fetch: func(ctx context.Context, url string, max int64, into *os.File) (string, int64, error) {
			return api.Download(ctx, dl, url, ua, max, into)
		},
		State: st, Trash: trash, Report: d.setDeskStatus, Interval: d.deskInterval,
	})
	ctx, cancel := context.WithCancel(root)
	d.mu.Lock()
	d.desk, d.deskCancel, d.deskID = eng, cancel, id
	d.mu.Unlock()
	go eng.Run(ctx)
}

// deskInterval is the folder sync's pace between passes: brisk while the human is at the
// computer, calmer otherwise (her changes also come at once through the chat socket's frames).
func (d *Daemon) deskInterval() time.Duration {
	d.mu.Lock()
	defer d.mu.Unlock()
	switch d.presence {
	case Active:
		return 15 * time.Second
	case Idle:
		return time.Minute
	}
	return 2 * time.Minute
}

// setDeskStatus keeps the folder sync's news and tells the shell.
func (d *Daemon) setDeskStatus(st desksync.Status) {
	d.mu.Lock()
	d.deskSt = &st
	d.mu.Unlock()
	d.emit("status", d.status())
}

// pokeDesk asks the folder sync for a pass now.
func (d *Daemon) pokeDesk() {
	d.mu.Lock()
	eng := d.desk
	d.mu.Unlock()
	if eng != nil {
		eng.Poke()
	}
}

// reconcileSocket opens or closes the chat socket to match paired × companion × presence.
func (d *Daemon) reconcileSocket() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.root == nil {
		return
	}
	k, _ := d.key(d.cfg.Base)
	want := k != "" && d.cfg.Companion != "" && d.presence == Active
	id := d.cfg.Base + "|" + d.cfg.Companion
	if d.sock != nil && (!want || d.sockKey != id) {
		d.sock()
		d.sock = nil
		d.sockKey = ""
	}
	if !want || d.sock != nil {
		return
	}
	c := d.newClient(d.cfg.Base, k, d.ua())
	u, err := c.ChatURL(d.cfg.Companion, d.tz)
	if err != nil {
		d.setSockLocked(chat.Stopped, err.Error())
		return
	}
	ctx, cancel := context.WithCancel(d.root)
	d.sock, d.sockKey = cancel, id
	opts := chat.Options{
		URL:    u,
		Header: c.Header(),
		Frame: func(f map[string]any) {
			t, _ := f["type"].(string)
			if forwarded[t] {
				d.emit("chat", f)
			}
			if t == "desktop" {
				d.pokeDesk()
			}
		},
		State: func(s chat.State, why string) {
			d.mu.Lock()
			d.setSockLocked(s, why)
			if (s == chat.AuthFailed || s == chat.Gone) && d.sockKey == id {
				// the socket stopped itself for good; forget it so a later reconcile can retry
				// after the human pairs again or picks another companion
				d.sock = nil
				d.sockKey = ""
			}
			d.mu.Unlock()
		},
	}
	go d.dial(ctx, opts)
}

func (d *Daemon) setSockLocked(s chat.State, why string) {
	if d.sockSt == s && d.sockWhy == why {
		return
	}
	d.sockSt, d.sockWhy = s, why
	d.emit("socket", map[string]string{"state": string(s), "reason": why})
}

// Status is what `status` answers.
type Status struct {
	Version   string `json:"version"`
	Base      string `json:"base"`
	Device    string `json:"device"`
	Paired    bool   `json:"paired"`
	Companion string `json:"companion"`
	KeyUUID   string `json:"key_uuid,omitempty"` // the device key's id on the site, for revoking it
	Presence  string `json:"presence"`
	Socket    string `json:"socket"`
	Reason    string `json:"reason,omitempty"`
	Keychain  string `json:"keychain,omitempty"` // an error reading the OS store, if any
	// the folder that holds a companion's desk on this computer, and that companion (desksync)
	DeskRoot      string           `json:"desk_root,omitempty"`
	DeskCompanion string           `json:"desk_companion,omitempty"`
	Desk          *desksync.Status `json:"desk,omitempty"` // what the folder sync is doing
}

func (d *Daemon) status() Status {
	d.mu.Lock()
	defer d.mu.Unlock()
	st := Status{
		Version: d.version, Base: d.cfg.Base, Device: d.cfg.Device, Companion: d.cfg.Companion,
		KeyUUID: d.cfg.KeyUUID, Presence: d.presence, Socket: string(d.sockSt), Reason: d.sockWhy,
		DeskRoot: d.cfg.DeskRoot, DeskCompanion: d.cfg.DeskCompanion, Desk: d.deskSt,
	}
	k, err := d.key(d.cfg.Base)
	if err != nil {
		st.Keychain = err.Error()
	}
	st.Paired = k != ""
	return st
}

// Handle serves one IPC request.
func (d *Daemon) Handle(ctx context.Context, c *ipc.Conn, method string, params json.RawMessage) (any, error) {
	switch method {
	case "status":
		return d.status(), nil

	case "subscribe":
		c.Subscribe()
		return d.status(), nil

	case "pair":
		var p struct {
			Token     string `json:"token"`
			Base      string `json:"base"`
			Device    string `json:"device"`
			Companion string `json:"companion"`
			KeyUUID   string `json:"key_uuid"`
		}
		if err := json.Unmarshal(params, &p); err != nil || !strings.HasPrefix(p.Token, "swm_") {
			return nil, ipc.Errorf("invalid_params", "pair needs {token: \"swm_…\"}")
		}
		return d.pair(ctx, p.Token, p.Base, p.Device, p.Companion, p.KeyUUID)

	case "unpair":
		return d.unpair(ctx)

	case "shutdown":
		if d.stop == nil {
			return nil, ipc.Errorf("not_allowed", "this nemd can't be stopped over IPC")
		}
		go d.stop() // after the answer is written
		return map[string]bool{"stopping": true}, nil

	case "key":
		d.mu.Lock()
		base := d.cfg.Base
		d.mu.Unlock()
		k, err := d.key(base)
		if err != nil {
			return nil, ipc.Errorf("keychain_unavailable", "%v", err)
		}
		if k == "" {
			return nil, ipc.Errorf("not_paired", "this computer isn't paired with %s yet", base)
		}
		return map[string]string{"key": k, "base": base}, nil

	case "companions":
		cl, _, err := d.client()
		if err != nil {
			return nil, err
		}
		list, err := cl.Companions(ctx)
		if err != nil {
			return nil, d.apiErr(err)
		}
		return map[string]any{"companions": list}, nil

	case "companion.select":
		var p struct {
			UUID string `json:"uuid"`
		}
		if err := json.Unmarshal(params, &p); err != nil || p.UUID == "" {
			return nil, ipc.Errorf("invalid_params", "companion.select needs {uuid}")
		}
		d.mu.Lock()
		d.cfg.Companion = p.UUID
		cfg := *d.cfg
		d.mu.Unlock()
		if err := d.persist(&cfg); err != nil {
			return nil, err
		}
		d.reconcile()
		return d.status(), nil

	case "presence.set":
		var p struct {
			State string `json:"state"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, ipc.Errorf("invalid_params", "presence.set needs {state}")
		}
		switch p.State {
		case Active, Idle, Away, Locked:
		default:
			return nil, ipc.Errorf("invalid_params", "presence is active, idle, away or locked")
		}
		d.mu.Lock()
		changed := d.presence != p.State
		d.presence = p.State
		d.mu.Unlock()
		if changed {
			d.reconcile()
			if p.State == Active {
				d.pokeDesk()
			}
		}
		return d.status(), nil

	case "desk.set":
		// the human picked (or made) a folder for the shown companion's desk: an empty one, or one
		// already hers (desksync.Check); it is marked, kept in the config, and the sync starts there
		var p struct {
			Root string `json:"root"`
		}
		if err := json.Unmarshal(params, &p); err != nil || strings.TrimSpace(p.Root) == "" {
			return nil, ipc.Errorf("invalid_params", "desk.set needs {root}")
		}
		d.mu.Lock()
		cfg := *d.cfg
		d.mu.Unlock()
		if cfg.Companion == "" {
			return nil, ipc.Errorf("no_companion", "no companion chosen")
		}
		if err := desksync.Claim(p.Root, cfg.Companion, cfg.Base); err != nil {
			var fe *desksync.FolderError
			if errors.As(err, &fe) {
				return nil, ipc.Errorf(fe.Code, "%s", fe.Path)
			}
			return nil, ipc.Errorf("folder_unusable", "%v", err)
		}
		cfg.DeskRoot, cfg.DeskCompanion = filepath.Clean(p.Root), cfg.Companion
		if err := d.persist(&cfg); err != nil {
			return nil, err
		}
		d.mu.Lock()
		d.cfg.DeskRoot, d.cfg.DeskCompanion = cfg.DeskRoot, cfg.DeskCompanion
		d.mu.Unlock()
		d.reconcileDesk()
		st := d.status()
		d.emit("status", st)
		return st, nil

	case "desk.unset":
		// stop syncing: the folder and its files stay where they are (and stay hers: re-linked if
		// picked again)
		if err := d.forgetDesk(); err != nil {
			return nil, err
		}
		d.reconcileDesk()
		st := d.status()
		d.emit("status", st)
		return st, nil

	case "desk.answer":
		// many files deleted from the folder at once: yes deletes them on her desk too, no puts
		// them back in the folder
		var p struct {
			Allow bool `json:"allow"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, ipc.Errorf("invalid_params", "desk.answer needs {allow}")
		}
		d.mu.Lock()
		eng := d.desk
		d.mu.Unlock()
		if eng == nil {
			return nil, ipc.Errorf("not_syncing", "no folder is syncing")
		}
		eng.Answer(p.Allow)
		return d.status(), nil

	case "pet.get", "pet.pet":
		cl, cfg, err := d.client()
		if err != nil {
			return nil, err
		}
		if cfg.Companion == "" {
			return nil, ipc.Errorf("no_companion", "no companion chosen")
		}
		var raw json.RawMessage
		if method == "pet.get" {
			raw, err = cl.Pet(ctx, cfg.Companion)
		} else {
			raw, err = cl.PetPet(ctx, cfg.Companion)
		}
		if err != nil {
			return nil, d.apiErr(err)
		}
		return raw, nil
	}
	return nil, ipc.Errorf("unknown_method", "%q", method)
}

// pair stores a freshly minted key (the shell got it from the logged-in page), checks it works,
// and picks a companion when there's only one.
func (d *Daemon) pair(ctx context.Context, token, base, device, companion, keyUUID string) (any, error) {
	d.mu.Lock()
	cfg := *d.cfg
	d.mu.Unlock()
	if base != "" {
		b, err := config.ValidBase(base)
		if err != nil {
			return nil, ipc.Errorf("invalid_params", "%v", err)
		}
		cfg.Base = b
	}
	if device != "" {
		cfg.Device = device
	}
	cfg.KeyUUID = keyUUID
	// Check the key before keeping it: a typo or a revoked key must not be stored.
	cl := d.newClient(cfg.Base, token, d.ua())
	list, err := cl.Companions(ctx)
	if err != nil {
		return nil, d.apiErr(err)
	}
	if err := d.keys.Set(config.Account(cfg.Base), token); err != nil {
		return nil, ipc.Errorf("keychain_unavailable", "%v", err)
	}
	switch {
	case companion != "":
		cfg.Companion = companion
	case len(list) == 1:
		cfg.Companion = list[0].UUID
	case !hasCompanion(list, cfg.Companion):
		cfg.Companion = ""
	}
	if err := d.persist(&cfg); err != nil {
		return nil, err
	}
	d.mu.Lock()
	d.cfg = &cfg
	d.mu.Unlock()
	d.emit("paired", map[string]any{"base": cfg.Base, "companion": cfg.Companion})
	d.reconcile()
	return map[string]any{"status": d.status(), "companions": list}, nil
}

func hasCompanion(list []api.Companion, uuid string) bool {
	for _, c := range list {
		if c.UUID == uuid {
			return true
		}
	}
	return false
}

// unpair forgets the key on this device. Revoking it on the site is the shell's job (DELETE
// /keys/:key_uuid with the page's session, before it calls this) or the human's, from Settings.
func (d *Daemon) unpair(_ context.Context) (any, error) {
	d.mu.Lock()
	base := d.cfg.Base
	cfg := *d.cfg
	d.mu.Unlock()
	if err := d.keys.Delete(config.Account(base)); err != nil && !errors.Is(err, keychain.ErrNotFound) {
		return nil, ipc.Errorf("keychain_unavailable", "%v", err)
	}
	cfg.KeyUUID = ""
	cfg.DeskRoot, cfg.DeskCompanion = "", "" // signed out: the folder stops syncing, its files stay
	if err := d.persist(&cfg); err != nil {
		return nil, err
	}
	d.mu.Lock()
	d.cfg.KeyUUID = ""
	d.cfg.DeskRoot, d.cfg.DeskCompanion = "", ""
	d.mu.Unlock()
	d.emit("unpaired", map[string]any{"base": base})
	d.reconcile()
	return d.status(), nil
}

// forgetDesk stops the folder sync (the folder and its files stay).
func (d *Daemon) forgetDesk() error {
	d.mu.Lock()
	cfg := *d.cfg
	d.mu.Unlock()
	cfg.DeskRoot, cfg.DeskCompanion = "", ""
	if err := d.persist(&cfg); err != nil {
		return err
	}
	d.mu.Lock()
	d.cfg.DeskRoot, d.cfg.DeskCompanion = "", ""
	d.mu.Unlock()
	return nil
}

func (d *Daemon) persist(cfg *config.Config) error {
	if d.save == nil {
		return nil
	}
	if err := d.save(cfg); err != nil {
		return ipc.Errorf("config_unwritable", "%v", err)
	}
	return nil
}

// apiErr turns an API failure into a coded IPC error; a refused key also tells the shell.
func (d *Daemon) apiErr(err error) error {
	var e *api.Error
	if errors.As(err, &e) {
		code := e.Code
		if code == "" {
			code = "api_error"
		}
		if api.AuthFailed(err) {
			d.emit("auth", map[string]string{"error": code})
		}
		return ipc.Errorf(code, "%v", err)
	}
	return ipc.Errorf("network", "%v", err)
}
