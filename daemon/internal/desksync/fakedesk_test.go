package desksync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/PixiLab-ai/nem-agent/daemon/internal/api"
)

// fakeDesk is her desk in memory, with the server's rules the engine relies on: rev and seq on
// every change, if_rev, tombstones in the feed, moves keeping the uuid, her system files and the
// locked ones refusing what isn't the human's own hand.
type fakeDesk struct {
	mu     sync.Mutex
	seq    int64
	n      int
	rows   map[string]*fakeRow
	calls  []string // "put /a.md (sync)", "del /b.md", "mv /a → /b", "mkdir /x"
	failDL map[string]bool
	// cdnOverride: what the CDN hands back for a URL instead of the row's bytes (its cache)
	cdnOverride map[string][]byte
	maxFiles    int // > 0: a new file past this many live files is refused (quota_exceeded)
}

type fakeRow struct {
	f    api.DeskFile
	text string
	data []byte
}

func newFakeDesk() *fakeDesk {
	return &fakeDesk{rows: map[string]*fakeRow{}, failDL: map[string]bool{}}
}

func hexSHA(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

func (d *fakeDesk) bump(r *fakeRow) {
	d.seq++
	r.f.Seq = d.seq
	r.f.Rev++
	r.f.UpdatedAt = time.Now().Add(-time.Hour)
}

// her writes (the companion's tools), straight into the desk
func (d *fakeDesk) herText(p, body string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.put(p, "text", body, nil, "agent")
}

func (d *fakeDesk) herBlob(p string, data []byte) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.put(p, "blob", "", data, "agent")
}

// ensureDirs makes a folder row for every ancestor, as the server does on each write
func (d *fakeDesk) ensureDirs(p string) {
	parts := strings.Split(strings.Trim(p, "/"), "/")
	for i := 1; i < len(parts); i++ {
		dir := "/" + strings.Join(parts[:i], "/")
		if r := d.rows[dir]; r == nil || r.f.Deleted {
			if r == nil {
				r = &fakeRow{f: api.DeskFile{UUID: "dir" + dir, Path: dir}}
				d.rows[dir] = r
			}
			r.f.Kind, r.f.Deleted = "dir", false
			d.bump(r)
		}
	}
}

func (d *fakeDesk) put(p, kind, text string, data []byte, origin string) *fakeRow {
	d.ensureDirs(p)
	r := d.rows[p]
	if r == nil {
		d.n++
		r = &fakeRow{f: api.DeskFile{UUID: "u" + strings.Repeat("0", 3) + string(rune('a'+d.n%26)) + time.Now().Format(".000000000") + p, Path: p}}
		d.rows[p] = r
	}
	r.f.Kind, r.f.Deleted, r.f.Origin = kind, false, origin
	r.f.Protected = isProtected(p)
	if kind == "text" {
		r.text, r.data = text, nil
		r.f.SHA256, r.f.Size, r.f.URL = hexSHA([]byte(text)), int64(len(text)), ""
	} else {
		r.data = append([]byte(nil), data...)
		r.f.SHA256, r.f.Size, r.f.URL = hexSHA(data), int64(len(data)), "https://cdn.test/"+r.f.UUID
	}
	d.bump(r)
	return r
}

func (d *fakeDesk) herDelete(p string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.del(p)
}

func (d *fakeDesk) del(p string) int {
	n := 0
	for q, r := range d.rows {
		if (q == p || strings.HasPrefix(q, p+"/")) && !r.f.Deleted {
			r.f.Deleted = true
			d.bump(r)
			n++
		}
	}
	return n
}

func (d *fakeDesk) herMove(from, to string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.mv(from, to)
}

func (d *fakeDesk) mv(from, to string) *fakeRow {
	d.ensureDirs(to)
	r := d.rows[from]
	delete(d.rows, from)
	r.f.Path = to
	d.rows[to] = r
	d.bump(r)
	return r
}

func (d *fakeDesk) lock(p string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	r := d.rows[p]
	r.f.Locked = true
	d.bump(r)
}

func (d *fakeDesk) live(p string) *fakeRow {
	d.mu.Lock()
	defer d.mu.Unlock()
	if r := d.rows[p]; r != nil && !r.f.Deleted {
		return r
	}
	return nil
}

func (d *fakeDesk) log(s string) { d.calls = append(d.calls, s) }

func (d *fakeDesk) DeskChanges(_ context.Context, _ string, since int64, limit int) (*api.DeskChanges, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	var items []api.DeskFile
	for _, r := range d.rows {
		if r.f.Seq > since {
			items = append(items, r.f)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Seq < items[j].Seq })
	if len(items) > limit {
		items = items[:limit]
	}
	next := since
	if len(items) > 0 {
		next = items[len(items)-1].Seq
	}
	return &api.DeskChanges{Items: items, NextSince: next, Head: d.seq}, nil
}

func (d *fakeDesk) DeskText(_ context.Context, _ string, p string) (*api.DeskFile, string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	r := d.rows[p]
	if r == nil || r.f.Deleted || r.f.Kind != "text" {
		return nil, "", &api.Error{Status: http.StatusNotFound, Code: "file_not_found"}
	}
	f := r.f
	return &f, r.text, nil
}

func (d *fakeDesk) DeskPut(_ context.Context, _ string, in api.DeskPutIn) (*api.DeskFile, bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.log("put " + in.Path)
	if isProtected(in.Path) {
		return nil, false, &api.Error{Status: http.StatusForbidden, Code: "protected"}
	}
	r := d.rows[in.Path]
	if r != nil && !r.f.Deleted && in.IfRev > 0 && r.f.Rev != in.IfRev {
		return nil, false, &api.Error{Status: http.StatusConflict, Code: "rev_conflict"}
	}
	if d.maxFiles > 0 && (r == nil || r.f.Deleted) && d.liveFiles() >= d.maxFiles {
		return nil, false, &api.Error{Status: http.StatusRequestEntityTooLarge, Code: "quota_exceeded"}
	}
	var nr *fakeRow
	if in.Text != nil {
		nr = d.put(in.Path, "text", *in.Text, nil, api.OriginSync)
	} else {
		nr = d.put(in.Path, "blob", "", in.Bytes, api.OriginSync)
	}
	f := nr.f
	return &f, false, nil
}

func (d *fakeDesk) liveFiles() int {
	n := 0
	for _, r := range d.rows {
		if !r.f.Deleted && r.f.Kind != "dir" {
			n++
		}
	}
	return n
}

func (d *fakeDesk) DeskQuota(_ context.Context, _ string) (*api.DeskQuota, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return &api.DeskQuota{Files: int64(d.liveFiles()), MaxFiles: int64(d.maxFiles)}, nil
}

func (d *fakeDesk) DeskMkdir(_ context.Context, _ string, p string) (*api.DeskFile, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.log("mkdir " + p)
	r := d.rows[p]
	if r == nil || r.f.Deleted {
		d.n++
		r = &fakeRow{f: api.DeskFile{UUID: "dir" + p, Path: p, Kind: "dir"}}
		d.rows[p] = r
		r.f.Deleted = false
		d.bump(r)
	}
	f := r.f
	return &f, nil
}

func (d *fakeDesk) DeskMove(_ context.Context, _ string, from, to string, ifRev int) (*api.DeskFile, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.log("mv " + from + " → " + to)
	r := d.rows[from]
	if r == nil || r.f.Deleted {
		return nil, &api.Error{Status: http.StatusNotFound, Code: "file_not_found"}
	}
	if t := d.rows[to]; t != nil && !t.f.Deleted {
		return nil, &api.Error{Status: http.StatusConflict, Code: "exists"}
	}
	if ifRev > 0 && r.f.Rev != ifRev {
		return nil, &api.Error{Status: http.StatusConflict, Code: "rev_conflict"}
	}
	f := d.mv(from, to).f
	return &f, nil
}

func (d *fakeDesk) DeskDelete(_ context.Context, _ string, p string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.log("del " + p)
	r := d.rows[p]
	if r == nil || r.f.Deleted {
		return &api.Error{Status: http.StatusNotFound, Code: "file_not_found"}
	}
	if isProtected(p) {
		return &api.Error{Status: http.StatusForbidden, Code: "protected"}
	}
	if r.f.Locked { // origin sync is never the human's own hand
		return &api.Error{Status: http.StatusForbidden, Code: "locked"}
	}
	d.del(p)
	return nil
}

// fetch is the CDN: a blob's bytes by its URL
func (d *fakeDesk) fetch(_ context.Context, url string, max int64, into *os.File) (string, int64, error) {
	d.mu.Lock()
	var data []byte
	failed := d.failDL[url]
	base := url
	if i := strings.Index(url, "?nemini-v="); i >= 0 {
		base = url[:i]
	}
	for _, r := range d.rows {
		if r.f.URL == base {
			data = r.data
		}
	}
	if o, ok := d.cdnOverride[url]; ok {
		data = o
	}
	d.mu.Unlock()
	if failed || data == nil {
		return "", 0, &api.Error{Status: http.StatusNotFound, Code: "download_failed"}
	}
	n, err := io.Copy(into, strings.NewReader(string(data)))
	return hexSHA(data), n, err
}
