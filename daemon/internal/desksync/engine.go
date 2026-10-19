package desksync

// The engine: one folder, one companion's desk, kept in step both ways.
//
// A pass reads her changes since the last one (the feed is a state feed: each row once, at its
// latest), then walks the folder for the human's. What both sides last agreed on per path is the
// State; a side whose bytes still match it didn't change. Rules:
//   - bytes are exact both ways; a file keeps its path
//   - her deletes go to a local trash; an edit made here beats her delete and goes up again
//   - both changed → her version keeps the path, the folder's is kept beside it as a conflict copy
//     (and goes up as a new file)
//   - her system files (personality, lessons, privacy, the avatar and its faces) come down but
//     never go up: a change made to one here is undone
//   - deletes from the folder go up with origin "sync" (never the human's own hand: a locked file
//     refuses them) — and more than a handful at once wait for the human's yes
//   - a folder gone or unmounted, a key refused, the companion gone: the engine stops and keeps
//     every file, never reads it as "everything deleted"

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/PixiLab-ai/nem-agent/daemon/internal/api"
)

const (
	tmpPrefix = ".nemini-tmp-"
	textMax   = 1 << 20   // the desk keeps text up to 1 MiB; bigger goes up as a blob
	autoMax   = 32 << 20  // bigger files stay in the folder only (for now: later, after a yes)
	blobMax   = 128 << 20 // the desk's cap
	quietText = 2 * time.Second
	quietBlob = 10 * time.Second
	// more deletes than this in one pass wait for the human (or 10% of the files, if more)
	massDeleteMin   = 100
	massDeleteShare = 0.10
	pageSize        = 500
	retryPerPass    = 20 // her files that didn't come down, tried again each pass
)

// Remote is what the engine needs of the site: api.Client, or a fake desk in tests.
type Remote interface {
	DeskChanges(ctx context.Context, companion string, since int64, limit int) (*api.DeskChanges, error)
	DeskText(ctx context.Context, companion, path string) (*api.DeskFile, string, error)
	DeskPut(ctx context.Context, companion string, in api.DeskPutIn) (*api.DeskFile, bool, error)
	DeskMkdir(ctx context.Context, companion, path string) (*api.DeskFile, error)
	DeskMove(ctx context.Context, companion, from, to string, ifRev int) (*api.DeskFile, error)
	DeskDelete(ctx context.Context, companion, path string) error
	DeskQuota(ctx context.Context, companion string) (*api.DeskQuota, error)
}

// Fetcher downloads a blob's bytes from its URL into a file: their sha256 and size.
type Fetcher func(ctx context.Context, url string, max int64, into *os.File) (string, int64, error)

// Status is what the engine says about itself (nemd's status, the settings window).
type Status struct {
	State    string    `json:"state"` // starting · downloading · up_to_date · syncing · waiting · paused · error · stopped
	Done     int       `json:"done,omitempty"`
	Total    int       `json:"total,omitempty"`
	Detail   string    `json:"detail,omitempty"` // waiting: the question; paused, error: why
	Count    int       `json:"count,omitempty"`  // waiting on mass_delete: how many files
	LastSync time.Time `json:"last_sync,omitempty"`
}

// Options set one engine up.
type Options struct {
	Root, Companion, Device string
	Remote                  Remote
	Fetch                   Fetcher
	State                   *State
	Trash                   string // the local trash: her deletes land there
	Report                  func(Status)
	Interval                func() time.Duration // between passes (shorter while the human is active)
	Rate, Burst             float64              // API calls a second, and the burst (default 2, 20)
}

// Engine keeps one folder and one companion's desk in step.
type Engine struct {
	o    Options
	s    *State
	pace *pacer
	poke chan struct{}

	mu        sync.Mutex
	status    Status
	allowMass bool      // the human said yes to the deletes waiting
	putBack   bool      // the human said no: those files come back from her desk
	waiting   []string  // the deletes waiting for that answer
	lastSent  time.Time // the last progress report
}

// errStop — the engine stops for good (until it's started again): a refused key, the companion gone.
type errStop struct{ code string }

func (e *errStop) Error() string { return "desksync: " + e.code }

// errPaused — the folder isn't there (unmounted, renamed, its marker gone): try again later.
var errPaused = errors.New("desksync: folder missing")

func New(o Options) *Engine {
	if o.Rate <= 0 {
		o.Rate = 2
	}
	if o.Burst <= 0 {
		o.Burst = 20
	}
	if o.Interval == nil {
		o.Interval = func() time.Duration { return time.Minute }
	}
	if o.Report == nil {
		o.Report = func(Status) {}
	}
	return &Engine{o: o, s: o.State, pace: newPacer(o.Rate, o.Burst), poke: make(chan struct{}, 1)}
}

// Poke asks for a pass now (her desk changed: a frame on the chat socket).
func (e *Engine) Poke() {
	select {
	case e.poke <- struct{}{}:
	default:
	}
}

// Answer is the human's answer to the deletes waiting: yes sends them up, no brings the files back.
func (e *Engine) Answer(allow bool) {
	e.mu.Lock()
	if allow {
		e.allowMass = true
	} else {
		e.putBack = true
	}
	e.mu.Unlock()
	e.Poke()
}

// Status is the last thing the engine said.
func (e *Engine) Status() Status {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.status
}

func (e *Engine) report(st Status) {
	e.mu.Lock()
	e.status = st
	throttled := st.State == "downloading" && time.Since(e.lastSent) < 500*time.Millisecond && st.Done < st.Total
	if !throttled {
		e.lastSent = time.Now()
	}
	e.mu.Unlock()
	if !throttled {
		e.o.Report(st)
	}
}

// Run keeps passing until ctx ends or the engine has to stop.
func (e *Engine) Run(ctx context.Context) {
	backoff := 5 * time.Second
	for ctx.Err() == nil {
		err := e.pass(ctx)
		var stop *errStop
		var wait time.Duration
		switch {
		case err == nil:
			backoff = 5 * time.Second
			wait = e.o.Interval()
		case errors.As(err, &stop):
			e.report(Status{State: "stopped", Detail: stop.code})
			return
		case errors.Is(err, errPaused):
			e.report(Status{State: "paused", Detail: "folder_missing"})
			wait = time.Minute
		case ctx.Err() != nil:
			return
		default:
			e.report(Status{State: "error", Detail: errCode(err)})
			wait = backoff
			if backoff < 5*time.Minute {
				backoff *= 2
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-e.poke:
		case <-time.After(wait):
		}
	}
}

func errCode(err error) string {
	var ae *api.Error
	if errors.As(err, &ae) {
		if ae.Code != "" {
			return ae.Code
		}
		return fmt.Sprintf("http_%d", ae.Status)
	}
	return "network"
}

// pass is one round: the folder is there, her changes come down, the human's go up.
func (e *Engine) pass(ctx context.Context) error {
	if err := e.checkRoot(); err != nil {
		return err
	}
	if !e.s.Ready {
		if err := e.coldStart(ctx); err != nil {
			return err
		}
	}
	if err := e.pull(ctx); err != nil {
		return err
	}
	if err := e.retryMissing(ctx); err != nil {
		return err
	}
	if err := e.push(ctx); err != nil {
		return err
	}
	if st := e.Status(); st.State != "waiting" {
		if n := len(e.s.RoomWait); n > 0 {
			// her desk is full: these stay in the folder only until it has room
			e.report(Status{State: "full", Count: n, LastSync: time.Now()})
		} else {
			e.report(Status{State: "up_to_date", LastSync: time.Now()})
		}
	}
	return nil
}

// checkRoot — the folder is there and still hers (its marker): else nothing is read as deleted.
func (e *Engine) checkRoot() error {
	info, err := os.Stat(e.o.Root)
	if err != nil || !info.IsDir() {
		return errPaused
	}
	m, err := ReadMarker(e.o.Root)
	if err != nil || m.Companion != e.o.Companion {
		return errPaused
	}
	return nil
}

// call runs one API call inside the pace; a 429 waits and tries again, a refused key or a gone
// companion stops the engine.
func (e *Engine) call(ctx context.Context, fn func() error) error {
	for attempt := 0; ; attempt++ {
		if err := e.pace.wait(ctx); err != nil {
			return err
		}
		err := fn()
		var ae *api.Error
		if errors.As(err, &ae) {
			switch {
			case ae.Status == http.StatusTooManyRequests && attempt < 5:
				e.pace.hold(time.Duration(5<<attempt) * time.Second)
				continue
			case ae.Status == http.StatusUnauthorized:
				return &errStop{"auth_failed"}
			case ae.Status == http.StatusNotFound && ae.Code == "companion_not_found":
				return &errStop{"companion_gone"}
			}
		}
		return err
	}
}

// --- her side: the first download and the changes since ------------------------------------

func (e *Engine) coldStart(ctx context.Context) error {
	e.report(Status{State: "starting"})
	rows := map[string]api.DeskFile{}
	var since, head int64
	for {
		var page *api.DeskChanges
		if err := e.call(ctx, func() (err error) {
			page, err = e.o.Remote.DeskChanges(ctx, e.o.Companion, since, pageSize)
			return
		}); err != nil {
			return err
		}
		for _, it := range page.Items {
			if it.Deleted {
				delete(rows, it.Path)
			} else {
				rows[it.Path] = it
			}
		}
		head = page.Head
		if page.NextSince > since {
			since = page.NextSince
		}
		if len(page.Items) == 0 || since >= head {
			break
		}
	}
	// folders, then text, then blobs smallest first
	list := make([]api.DeskFile, 0, len(rows))
	for p, f := range rows {
		if !ignored(p) {
			list = append(list, f)
		}
	}
	rank := func(f api.DeskFile) int {
		switch f.Kind {
		case "dir":
			return 0
		case "text":
			return 1
		}
		return 2
	}
	sort.Slice(list, func(i, j int) bool {
		a, b := list[i], list[j]
		if rank(a) != rank(b) {
			return rank(a) < rank(b)
		}
		if a.Kind == "blob" && a.Size != b.Size {
			return a.Size < b.Size
		}
		return a.Path < b.Path
	})
	total := 0
	for _, f := range list {
		if f.Kind != "dir" {
			total++
		}
	}
	done := 0
	for _, f := range list {
		if err := e.materialize(ctx, f); err != nil {
			return err
		}
		if f.Kind != "dir" {
			done++
			e.report(Status{State: "downloading", Done: done, Total: total})
		}
	}
	e.s.Cursor = since
	e.s.Ready = true
	return e.s.Save()
}

// materialize puts one of her files in the folder: a file already there with the same bytes is
// adopted (a re-linked folder); one with other bytes is kept beside as a conflict copy.
func (e *Engine) materialize(ctx context.Context, f api.DeskFile) error {
	local, ok := e.localPath(f.Path)
	if !ok {
		e.s.Entries[f.Path] = &Entry{UUID: f.UUID, Kind: f.Kind, Rev: f.Rev, SHA: f.SHA256, URL: f.URL, Mime: f.Mime, Missing: "name"}
		return nil
	}
	if f.Kind == "dir" {
		if err := os.MkdirAll(local, 0o755); err != nil {
			return err
		}
		e.s.Entries[f.Path] = &Entry{UUID: f.UUID, Kind: "dir", Rev: f.Rev}
		return nil
	}
	if info, err := os.Lstat(local); err == nil && info.Mode().IsRegular() {
		if sum, err := fileSHA(local); err == nil && !f.ByReference() && sum == f.SHA256 {
			return e.agree(f, local, sum)
		}
		if err := e.conflictCopy(local); err != nil {
			return err
		}
	}
	return e.fetchInto(ctx, f, local)
}

// fetchInto downloads her version of f into local (atomically: a temp file renamed over it).
func (e *Engine) fetchInto(ctx context.Context, f api.DeskFile, local string) error {
	if err := os.MkdirAll(filepath.Dir(local), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(local), tmpPrefix+"*")
	if err != nil {
		return err
	}
	keep := false
	defer func() {
		if !keep {
			tmp.Close()
			os.Remove(tmp.Name())
		}
	}()
	var sum string
	transformed := false
	switch f.Kind {
	case "text":
		var row *api.DeskFile
		var body string
		if err := e.call(ctx, func() (err error) {
			row, body, err = e.o.Remote.DeskText(ctx, e.o.Companion, f.Path)
			return
		}); err != nil {
			var ae *api.Error
			if errors.As(err, &ae) && ae.Status == http.StatusNotFound {
				return nil // gone meanwhile: the next pull says so
			}
			return err
		}
		f = *row
		if _, err := tmp.WriteString(body); err != nil {
			return err
		}
		sum = shaHex([]byte(body))
	case "blob":
		if f.URL == "" || e.o.Fetch == nil {
			e.s.Entries[f.Path] = &Entry{UUID: f.UUID, Kind: f.Kind, Rev: f.Rev, SHA: f.SHA256, URL: f.URL, Mime: f.Mime, Missing: "no_url"}
			return nil
		}
		s, _, err := e.o.Fetch(ctx, f.URL, blobMax, tmp)
		if err == nil && !f.ByReference() && s != f.SHA256 {
			// The CDN may answer with a picture it re-compressed in its cache (2026-10-19: 490
			// bytes for a 736-byte PNG, no header saying so): this version once more, past the
			// cache; still other bytes for a picture = the same picture, kept as it came.
			if _, serr := tmp.Seek(0, io.SeekStart); serr == nil && tmp.Truncate(0) == nil {
				s, _, err = e.o.Fetch(ctx, pastCache(f.URL, f.SHA256), blobMax, tmp)
			}
			if err == nil && s != f.SHA256 && isPicture(f) {
				transformed = true
			}
		}
		if err != nil || (!f.ByReference() && s != f.SHA256 && !transformed) {
			why := "download_failed"
			if errors.Is(err, api.ErrTooBig) {
				why = "too_large"
			} else if err == nil {
				why = "checksum"
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			e.s.Entries[f.Path] = &Entry{UUID: f.UUID, Kind: f.Kind, Rev: f.Rev, SHA: f.SHA256, URL: f.URL, Mime: f.Mime, Missing: why}
			return nil
		}
		sum = s
	default:
		return nil
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), local); err != nil {
		return err
	}
	keep = true
	_ = os.Chmod(local, 0o644) // a temp file is the user's only: a file in the folder is like any other
	if !f.UpdatedAt.IsZero() {
		_ = os.Chtimes(local, time.Now(), f.UpdatedAt)
	}
	return e.agree(f, local, sum)
}

// retryMissing tries again, a few per pass, her files that didn't come down (a network blip, the
// CDN answering something else under a burst): a missing file is never read as a delete, but it
// shouldn't stay missing either.
func (e *Engine) retryMissing(ctx context.Context) error {
	n := 0
	for p, en := range e.s.Entries {
		if en.Missing != "download_failed" && en.Missing != "checksum" {
			continue
		}
		if n >= retryPerPass {
			break
		}
		n++
		local, ok := e.localPath(p)
		if !ok || exists(local) {
			continue // something is there now: the scan looks at it
		}
		f := api.DeskFile{UUID: en.UUID, Path: p, Kind: en.Kind, Rev: en.Rev, SHA256: en.SHA, URL: en.URL, Mime: en.Mime, Meta: en.Meta, Protected: en.Protected, Locked: en.Locked}
		if err := e.fetchInto(ctx, f, local); err != nil {
			return err
		}
	}
	return nil
}

// isPicture — a picture, by its mime or, for an entry that doesn't know it, by its name.
func isPicture(f api.DeskFile) bool {
	m := f.Mime
	if m == "" {
		m = mime.TypeByExtension(strings.ToLower(path.Ext(f.Path)))
	}
	return strings.HasPrefix(m, "image/")
}

// pastCache is a blob's URL asking for this version of it past the CDN's cache.
func pastCache(rawURL, sha string) string {
	sep := "?"
	if strings.Contains(rawURL, "?") {
		sep = "&"
	}
	if len(sha) > 16 {
		sha = sha[:16]
	}
	return rawURL + sep + "nemini-v=" + sha
}

// agree records that the folder and her desk now hold the same thing at f.Path.
func (e *Engine) agree(f api.DeskFile, local, localSHA string) error {
	info, err := os.Stat(local)
	if err != nil {
		return err
	}
	e.s.Entries[f.Path] = &Entry{
		UUID: f.UUID, Kind: f.Kind, Rev: f.Rev, SHA: f.SHA256, LocalSHA: localSHA, Size: info.Size(),
		MTime: info.ModTime().UnixNano(), Meta: f.Meta, URL: f.URL, Mime: f.Mime, Protected: f.Protected || isProtected(f.Path), Locked: f.Locked,
	}
	delete(e.s.LocalOnly, f.Path)
	return nil
}

// pull applies her changes since the cursor, page by page, in order.
func (e *Engine) pull(ctx context.Context) error {
	for {
		var page *api.DeskChanges
		if err := e.call(ctx, func() (err error) {
			page, err = e.o.Remote.DeskChanges(ctx, e.o.Companion, e.s.Cursor, pageSize)
			return
		}); err != nil {
			return err
		}
		uuids := e.s.byUUID()
		for _, it := range page.Items {
			if err := e.applyRemote(ctx, it, uuids); err != nil {
				return err
			}
			if it.Seq > e.s.Cursor {
				e.s.Cursor = it.Seq
			}
		}
		if page.NextSince > e.s.Cursor {
			e.s.Cursor = page.NextSince
		}
		if err := e.s.Save(); err != nil {
			return err
		}
		if len(page.Items) == 0 || e.s.Cursor >= page.Head {
			return nil
		}
	}
}

func (e *Engine) applyRemote(ctx context.Context, it api.DeskFile, uuids map[string]string) error {
	if ignored(it.Path) {
		return nil
	}
	base := e.s.Entries[it.Path]
	if it.Deleted {
		if base == nil || (it.UUID != "" && base.UUID != "" && base.UUID != it.UUID) {
			return nil
		}
		delete(e.s.Entries, it.Path)
		delete(uuids, base.UUID)
		local, ok := e.localPath(it.Path)
		if !ok {
			return nil
		}
		if base.Kind == "dir" {
			removeEmptyTree(local) // only empty folders go: a file left in it goes up again
			return nil
		}
		if e.unchanged(local, base) {
			return e.toTrash(local, it.Path)
		}
		return nil // edited here: the edit wins, it goes up as a new file
	}
	// her move or rename: the same uuid, known at another path
	if old, ok := uuids[it.UUID]; ok && old != it.Path {
		ob := e.s.Entries[old]
		delete(e.s.Entries, old)
		from, okFrom := e.localPath(old)
		to, okTo := e.localPath(it.Path)
		if ob != nil && ob.Kind == "dir" && okFrom && exists(to) {
			// the feed brings a folder's files before the folder itself: they're already at the
			// new place, the old folder is left empty
			removeEmptyTree(from)
		}
		if ob != nil && okFrom && okTo && !exists(to) && (ob.Kind == "dir" || e.unchanged(from, ob)) {
			if err := os.MkdirAll(filepath.Dir(to), 0o755); err == nil && os.Rename(from, to) == nil {
				moved := *ob
				e.s.Entries[it.Path] = &moved
				base = &moved
				if ob.Kind == "dir" {
					for p, en := range e.s.Entries {
						if strings.HasPrefix(p, old+"/") {
							delete(e.s.Entries, p)
							e.s.Entries[it.Path+strings.TrimPrefix(p, old)] = en
						}
					}
					uuids = e.s.byUUID()
				}
			}
		}
	}
	uuids[it.UUID] = it.Path
	local, ok := e.localPath(it.Path)
	if !ok {
		e.s.Entries[it.Path] = &Entry{UUID: it.UUID, Kind: it.Kind, Rev: it.Rev, SHA: it.SHA256, URL: it.URL, Mime: it.Mime, Missing: "name"}
		return nil
	}
	if it.Kind == "dir" {
		if err := os.MkdirAll(local, 0o755); err != nil {
			return err
		}
		e.s.Entries[it.Path] = &Entry{UUID: it.UUID, Kind: "dir", Rev: it.Rev}
		return nil
	}
	if base != nil && base.SHA == it.SHA256 && base.Missing == "" && exists(local) {
		// the same bytes: her meta, a lock, a rev
		base.UUID, base.Rev, base.Meta, base.URL, base.Locked = it.UUID, it.Rev, it.Meta, it.URL, it.Locked
		base.Protected = it.Protected || isProtected(it.Path)
		return nil
	}
	// new or changed on her side
	if info, err := os.Lstat(local); err == nil && info.Mode().IsRegular() && (base == nil || !e.unchanged(local, base)) {
		if sum, err := fileSHA(local); err == nil && !it.ByReference() && sum == it.SHA256 {
			return e.agree(it, local, sum) // the same change on both sides
		}
		if err := e.conflictCopy(local); err != nil {
			return err
		}
	}
	return e.fetchInto(ctx, it, local)
}

// --- the human's side: the folder's changes go up -------------------------------------------

type localFile struct {
	p    string // server form
	abs  string
	info fs.FileInfo
}

func (e *Engine) scan() (dirs []string, files []localFile, err error) {
	err = filepath.WalkDir(e.o.Root, func(abs string, d fs.DirEntry, werr error) error {
		if werr != nil {
			if abs == e.o.Root {
				return werr
			}
			return nil // unreadable: skipped this time
		}
		if abs == e.o.Root {
			return nil
		}
		rel, rerr := filepath.Rel(e.o.Root, abs)
		if rerr != nil {
			return nil
		}
		p := "/" + filepath.ToSlash(rel)
		if ignoredName(d.Name()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return nil // a link is never followed or sent
		}
		if d.IsDir() {
			dirs = append(dirs, p)
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		files = append(files, localFile{p: p, abs: abs, info: info})
		return nil
	})
	return
}

func (e *Engine) push(ctx context.Context) error {
	if err := e.checkRoom(ctx); err != nil {
		return err
	}
	dirs, files, err := e.scan()
	if err != nil {
		return errPaused
	}
	seen := make(map[string]bool, len(dirs)+len(files))
	// folders the desk doesn't have yet
	sort.Strings(dirs)
	for _, p := range dirs {
		seen[p] = true
		if en := e.s.Entries[p]; en != nil {
			continue
		}
		if strings.HasPrefix(path.Base(p), ".") {
			continue // a new dot-folder stays here
		}
		var f *api.DeskFile
		if err := e.call(ctx, func() (err error) { f, err = e.o.Remote.DeskMkdir(ctx, e.o.Companion, p); return }); err != nil {
			if e.refused(err) {
				continue
			}
			return err
		}
		e.s.Entries[p] = &Entry{UUID: f.UUID, Kind: "dir", Rev: f.Rev}
	}
	for _, lf := range files {
		seen[lf.p] = true
	}
	// gone from the folder (her files we couldn't download aside: never deletes)
	gone := map[string]*Entry{}
	goneBySHA := map[string]string{}
	for p, en := range e.s.Entries {
		if !seen[p] && en.Missing == "" {
			gone[p] = en
			if en.Kind != "dir" && en.LocalSHA != "" {
				goneBySHA[en.LocalSHA] = p
			}
		}
	}
	now := time.Now()
	for _, lf := range files {
		if err := e.pushFile(ctx, lf, now, gone, goneBySHA); err != nil {
			return err
		}
	}
	if err := e.pushDeletes(ctx, gone); err != nil {
		return err
	}
	return e.s.Save()
}

// pushFile sends one local file up if it changed since the last agreement.
func (e *Engine) pushFile(ctx context.Context, lf localFile, now time.Time, gone map[string]*Entry, goneBySHA map[string]string) error {
	base := e.s.Entries[lf.p]
	size, mtime := lf.info.Size(), lf.info.ModTime()
	if base != nil && base.Missing != "" {
		return nil
	}
	if base != nil && size == base.Size && mtime.UnixNano() == base.MTime {
		return nil
	}
	quiet := quietText
	if size > textMax {
		quiet = quietBlob
	}
	if now.Sub(mtime) < quiet {
		return nil // still being written: the next pass
	}
	sig := fmt.Sprintf("%d:%d", size, mtime.UnixNano())
	if e.s.LocalOnly[lf.p] == sig || e.s.RoomWait[lf.p] == sig {
		return nil // refused before, unchanged since (her desk full: until it has room)
	}
	if size > autoMax {
		e.s.LocalOnly[lf.p] = sig
		return nil
	}
	data, err := os.ReadFile(lf.abs)
	if err != nil {
		return nil // busy or gone: the next pass
	}
	sum := shaHex(data)
	if base != nil && sum == base.LocalSHA {
		base.Size, base.MTime = size, mtime.UnixNano() // touched, same bytes
		return nil
	}
	if isProtected(lf.p) || (base != nil && base.Protected) {
		// her system files are hers: what changed here is undone (D8)
		if base != nil {
			return e.restore(ctx, lf.p, base)
		}
		return nil
	}
	if base == nil && strings.HasPrefix(path.Base(lf.p), ".") {
		return nil // a new dot-file stays here (her known ones sync both ways)
	}
	// a move here: a new file with the bytes of one gone from the folder
	if base == nil {
		if from, ok := goneBySHA[sum]; ok {
			if moved, err := e.moveUp(ctx, from, lf, sum); err != nil {
				return err
			} else if moved {
				delete(gone, from)
				delete(goneBySHA, sum)
				return nil
			}
		}
	}
	in := api.DeskPutIn{Path: lf.p}
	if isText(data) && (base == nil || base.Kind != "blob") {
		s := string(data)
		in.Text = &s
	} else {
		in.Bytes = data
	}
	if base != nil {
		in.IfRev = base.Rev
		in.Meta = keptMeta(base.Meta, in.Text == nil)
	}
	var f *api.DeskFile
	err = e.call(ctx, func() (err error) { f, _, err = e.o.Remote.DeskPut(ctx, e.o.Companion, in); return })
	var ae *api.Error
	switch {
	case err == nil:
		return e.agree(*f, lf.abs, sum)
	case errors.As(err, &ae) && ae.Status == http.StatusConflict:
		return e.pull(ctx) // she changed it too: the pull keeps both versions
	case errors.As(err, &ae) && ae.Status == http.StatusForbidden && base != nil:
		return e.restore(ctx, lf.p, base) // her file, or locked: her version comes back
	case errors.As(err, &ae) && ae.Status == http.StatusRequestEntityTooLarge && ae.Code == "quota_exceeded":
		e.s.RoomWait[lf.p] = sig // her desk is full: it goes up once there's room
		return nil
	case e.refused(err):
		e.s.LocalOnly[lf.p] = sig
		return nil
	}
	return err
}

// checkRoom — files waiting for room on her desk: once it has some again (she deleted files),
// they're tried again on this pass.
func (e *Engine) checkRoom(ctx context.Context) error {
	if len(e.s.RoomWait) == 0 {
		return nil
	}
	var q *api.DeskQuota
	if err := e.call(ctx, func() (err error) { q, err = e.o.Remote.DeskQuota(ctx, e.o.Companion); return }); err != nil {
		return err
	}
	if (q.MaxFiles > 0 && q.Files >= q.MaxFiles) || (q.MaxBytes > 0 && q.Bytes >= q.MaxBytes) {
		return nil // still full
	}
	e.s.RoomWait = map[string]string{}
	return nil
}

// moveUp — a file renamed or moved in the folder becomes a move on her desk (her meta, her
// picture's words and its memory follow it): false when the desk won't, and it goes up as new.
func (e *Engine) moveUp(ctx context.Context, from string, lf localFile, sum string) (bool, error) {
	base := e.s.Entries[from]
	if base == nil || base.Protected || isProtected(from) || isProtected(lf.p) {
		return false, nil
	}
	var f *api.DeskFile
	err := e.call(ctx, func() (err error) { f, err = e.o.Remote.DeskMove(ctx, e.o.Companion, from, lf.p, base.Rev); return })
	if err != nil {
		if e.refused(err) {
			return false, nil
		}
		var ae *api.Error
		if errors.As(err, &ae) && ae.Status == http.StatusConflict {
			return false, nil
		}
		return false, err
	}
	delete(e.s.Entries, from)
	return true, e.agree(*f, lf.abs, sum)
}

// pushDeletes sends up what the folder no longer has — a handful at once, or after a yes.
func (e *Engine) pushDeletes(ctx context.Context, gone map[string]*Entry) error {
	e.mu.Lock()
	allow, putBack := e.allowMass, e.putBack
	e.allowMass, e.putBack = false, false
	e.mu.Unlock()
	if len(gone) == 0 {
		e.clearWaiting()
		return nil
	}
	if err := e.checkRoot(); err != nil {
		return err // the folder went away while we looked: nothing is deleted
	}
	files := 0
	for _, en := range gone {
		if en.Kind != "dir" {
			files++
		}
	}
	limit := massDeleteMin
	if l := int(massDeleteShare * float64(len(e.s.Entries))); l > limit {
		limit = l
	}
	if files > limit && !allow {
		if putBack {
			for p, en := range gone {
				if en.Kind == "dir" {
					if local, ok := e.localPath(p); ok {
						_ = os.MkdirAll(local, 0o755)
					}
					continue
				}
				if err := e.restore(ctx, p, en); err != nil {
					return err
				}
			}
			e.clearWaiting()
			return nil
		}
		e.mu.Lock()
		e.waiting = keys(gone)
		e.mu.Unlock()
		e.report(Status{State: "waiting", Detail: "mass_delete", Count: files})
		return nil
	}
	e.clearWaiting()
	// files first, then folders, deepest first
	paths := keys(gone)
	sort.Slice(paths, func(i, j int) bool {
		a, b := gone[paths[i]], gone[paths[j]]
		if (a.Kind == "dir") != (b.Kind == "dir") {
			return b.Kind == "dir"
		}
		return strings.Count(paths[i], "/") > strings.Count(paths[j], "/")
	})
	for _, p := range paths {
		en := gone[p]
		// her system files, and the files the human locked on her desk ("Protected"): never
		// deleted from here, whatever the server would let through — they come back
		if en.Protected || isProtected(p) || en.Locked {
			if err := e.restore(ctx, p, en); err != nil {
				return err
			}
			continue
		}
		if en.Kind == "dir" && e.hasEntriesUnder(p) {
			continue // something under it stays: so does the folder
		}
		err := e.call(ctx, func() error { return e.o.Remote.DeskDelete(ctx, e.o.Companion, p) })
		var ae *api.Error
		switch {
		case err == nil, errors.As(err, &ae) && ae.Status == http.StatusNotFound:
			delete(e.s.Entries, p)
		case errors.As(err, &ae) && ae.Status == http.StatusForbidden:
			if en.Kind == "dir" {
				if local, ok := e.localPath(p); ok {
					_ = os.MkdirAll(local, 0o755)
				}
				continue
			}
			if err := e.restore(ctx, p, en); err != nil { // locked: her file comes back
				return err
			}
		default:
			return err
		}
	}
	return nil
}

func (e *Engine) clearWaiting() {
	e.mu.Lock()
	had := len(e.waiting) > 0
	e.waiting = nil
	e.mu.Unlock()
	if had {
		e.report(Status{State: "syncing"})
	}
}

func (e *Engine) hasEntriesUnder(dir string) bool {
	for p := range e.s.Entries {
		if strings.HasPrefix(p, dir+"/") {
			return true
		}
	}
	return false
}

// restore brings her version of p back into the folder (a change to her system file, a delete
// the desk refused). What the folder had there goes to the local trash, not beside it: a copy in
// the folder would go up to her desk as a new file, and a change to her own files is ignored,
// never sent.
func (e *Engine) restore(ctx context.Context, p string, en *Entry) error {
	local, ok := e.localPath(p)
	if !ok {
		return nil
	}
	if exists(local) {
		if err := e.toTrash(local, p); err != nil {
			return err
		}
	}
	return e.fetchInto(ctx, api.DeskFile{UUID: en.UUID, Path: p, Kind: en.Kind, Rev: en.Rev, SHA256: en.SHA, URL: en.URL, Mime: en.Mime, Meta: en.Meta, Protected: en.Protected, Locked: en.Locked}, local)
}

// refused — an answer that won't change by trying again: the file stays here only.
func (e *Engine) refused(err error) bool {
	var ae *api.Error
	if !errors.As(err, &ae) {
		return false
	}
	return ae.Status == http.StatusRequestEntityTooLarge || ae.Status == http.StatusBadRequest || ae.Status == http.StatusForbidden
}

// --- files ----------------------------------------------------------------------------------

// localPath is p's place in the folder; false for a name this computer can't hold.
func (e *Engine) localPath(p string) (string, bool) {
	rel := strings.TrimPrefix(path.Clean("/"+p), "/")
	if rel == "" || rel == "." || !nameOK(rel) {
		return "", false
	}
	abs := filepath.Join(e.o.Root, filepath.FromSlash(rel))
	if r, err := filepath.Rel(e.o.Root, abs); err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return "", false
	}
	return abs, true
}

// nameOK — Windows can't hold some names her desk allows (research §3.10: mapped later).
func nameOK(rel string) bool {
	if runtime.GOOS != "windows" {
		return true
	}
	for _, seg := range strings.Split(rel, "/") {
		if strings.ContainsAny(seg, `<>:"|?*\`) || strings.HasSuffix(seg, ".") || strings.HasSuffix(seg, " ") {
			return false
		}
		stem := strings.ToUpper(strings.SplitN(seg, ".", 2)[0])
		switch stem {
		case "CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
			"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
			return false
		}
	}
	return true
}

// unchanged — the local file still holds what the folder and her desk agreed on.
func (e *Engine) unchanged(local string, en *Entry) bool {
	info, err := os.Stat(local)
	if err != nil {
		return false
	}
	if info.Size() == en.Size && info.ModTime().UnixNano() == en.MTime {
		return true
	}
	sum, err := fileSHA(local)
	if err != nil || sum != en.LocalSHA {
		return false
	}
	en.Size, en.MTime = info.Size(), info.ModTime().UnixNano()
	return true
}

// conflictCopy moves the folder's version aside, beside it: it goes up as a new file.
func (e *Engine) conflictCopy(local string) error {
	dir, name := filepath.Split(local)
	ext := path.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	device := strings.NewReplacer("/", "-", "\\", "-", ":", "-").Replace(e.o.Device)
	if device == "" {
		device = "this computer"
	}
	base := fmt.Sprintf("%s (conflict from %s %s)", stem, device, time.Now().Format("2006-01-02 1504"))
	to := filepath.Join(dir, base+ext)
	for i := 2; exists(to); i++ {
		to = filepath.Join(dir, fmt.Sprintf("%s %d%s", base, i, ext))
	}
	return os.Rename(local, to)
}

// toTrash keeps a file she deleted in the local trash (30 days), out of the folder.
func (e *Engine) toTrash(local, p string) error {
	if e.o.Trash == "" {
		return os.Remove(local)
	}
	dst := filepath.Join(e.o.Trash, time.Now().Format("20060102-150405"), filepath.FromSlash(strings.TrimPrefix(p, "/")))
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	if err := os.Rename(local, dst); err == nil {
		return nil
	}
	// another disk: copy, then remove
	src, err := os.Open(local)
	if err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		src.Close()
		return err
	}
	_, cerr := io.Copy(out, src)
	src.Close()
	if err := out.Close(); cerr == nil {
		cerr = err
	}
	if cerr != nil {
		return cerr
	}
	return os.Remove(local)
}

// removeEmptyTree removes dir and the folders under it, as long as no file is left in them.
func removeEmptyTree(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	empty := true
	for _, e := range entries {
		if !e.IsDir() || !removeEmptyTree(filepath.Join(dir, e.Name())) {
			empty = false
		}
	}
	if !empty {
		return false
	}
	return os.Remove(dir) == nil
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

func fileSHA(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func shaHex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// isText — goes up as text: valid UTF-8, no NUL, within the desk's text cap.
func isText(b []byte) bool {
	return len(b) <= textMax && utf8.Valid(b) && !bytes.Contains(b, []byte{0})
}

// keptMeta is the row's meta to send back with a write (a write without meta wipes it on the
// server today); new bytes for a blob drop what was derived from the old ones.
func keptMeta(meta json.RawMessage, blob bool) json.RawMessage {
	if len(meta) == 0 || !blob {
		return meta
	}
	var m map[string]any
	if json.Unmarshal(meta, &m) != nil {
		return nil
	}
	for _, k := range []string{"listen", "size", "size_probed_at"} {
		delete(m, k)
	}
	out, err := json.Marshal(m)
	if err != nil {
		return nil
	}
	return out
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
